// Package main implements the searchservice binary.
//
// The searchservice provides geocoding search for the SwayRider platform.
// It exposes:
//   - gRPC on port 8081 for internal service-to-service communication
//   - REST on port 8080 via grpc-gateway for HTTP API access
//
// # Configuration
//
//	HTTP_PORT              (default: 8080)
//	GRPC_PORT              (default: 8081)
//	PELIAS_REGIONS         e.g. "iberian-peninsula=http://host:3100/v1,west-europe=http://host:3100/v1"
//	REGION_SERVICE_HOST
//	REGION_SERVICE_PORT
//	AUTHSERVICE_HOST
//	AUTHSERVICE_PORT
//
// # Search Endpoint Security
//
// The /Search RPC requires a valid JWT (email verified).
// /Ping and /health endpoints are public.
package main

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/swayrider/grpcclients"
	"github.com/swayrider/grpcclients/authclient"
	"github.com/swayrider/grpcclients/regionclient"
	healthv1 "github.com/swayrider/protos/health/v1"
	searchv1 "github.com/swayrider/protos/search/v1"
	"github.com/swayrider/searchservice/internal/config"
	"github.com/swayrider/searchservice/internal/pelias"
	"github.com/swayrider/searchservice/internal/search"
	"github.com/swayrider/searchservice/internal/server"
	"github.com/swayrider/swlib/app"
	"github.com/swayrider/swlib/jwtkeys"
	log "github.com/swayrider/swlib/logger"
	"google.golang.org/grpc"
)

const (
	FldPeliasRegions     = "pelias-regions"
	EnvPeliasRegions     = "PELIAS_REGIONS"
	AppDataPeliasRegions = "pelias-regions-map"

	FldHealthProbeTtlSecs = "health-probe-ttl-secs"
	EnvHealthProbeTTLSecs = "HEALTH_PROBE_TTL_SECS"
	DefHealthProbeTtlSecs = 15
)

func main() {
	application := app.New("searchservice").
		WithDefaultConfigFields(app.BackendServiceFields, app.FlagGroupOverrides{}).
		WithServiceClients(
			app.NewServiceClient("authservice", authServiceClientCtor),
			app.NewServiceClient("regionservice", regionServiceClientCtor),
		).
		WithConfigFields(
			app.NewStringConfigField(
				FldPeliasRegions, EnvPeliasRegions,
				"Pelias region URLs (region=url,...)", ""),
			app.NewIntConfigField(
				FldHealthProbeTtlSecs, EnvHealthProbeTTLSecs,
				"How long in seconds a health probe result is cached before re-probing dependencies",
				DefHealthProbeTtlSecs),
		).
		WithConfigFields(app.RateLimitConfigFields()...).
		WithConfigFields(app.JWTKeysConfigFields()...)

	jwtKeyCache := jwtkeys.New(application.Logger())

	grpcConfig := app.NewGrpcConfig(
		app.AuthInterceptor|app.ClientInfoInterceptor|app.RateLimitInterceptor,
		jwtKeyCache.GetPublicKeys,
		app.GrpcServiceHooks{
			ServiceRegistrar:   grpcSearchRegistrar,
			ServiceHTTPHandler: grpcSearchGateway(application),
		},
		app.GrpcServiceHooks{
			ServiceRegistrar:   grpcHealthRegistrar,
			ServiceHTTPHandler: grpcHealthGateway(application),
		},
	)

	application = application.
		WithBackgroundRoutines(
			app.JWTKeysFetcher(jwtKeyCache),
			app.RateLimitEvictor(grpcConfig),
		).
		WithInitializers(bootstrapFn, app.JWTKeysInitializer(jwtKeyCache), app.RateLimiterInitializer(grpcConfig)).
		WithGrpc(grpcConfig)
	application.Run()
}

// bootstrapFn validates configuration on startup and stores the parsed Pelias
// region map so the gRPC registrar does not re-parse it.
func bootstrapFn(a app.App) error {
	lg := a.Logger().Derive(log.WithFunction("bootstrap"))
	lg.Infoln("Bootstrapping service ...")

	peliasRegions := app.GetConfigField[string](a.Config(), FldPeliasRegions)
	urlMap, err := config.ParsePeliasRegions(peliasRegions)
	if err != nil {
		panic(fmt.Sprintf("invalid PELIAS_REGIONS: %v", err))
	}
	app.SetAppData(a, AppDataPeliasRegions, urlMap)
	return nil
}

// authServiceClientCtor creates a new auth service gRPC client.
func authServiceClientCtor(a app.App) grpcclients.Client {
	lg := a.Logger().Derive(log.WithFunction("authServiceClientCtor"))
	clnt, err := authclient.New(
		app.ServiceClientHostAndPort(a, "authservice"))
	if err != nil {
		lg.Fatalf("failed to create authservice client: %v", err)
	}
	return clnt
}

// regionServiceClientCtor creates a new region service gRPC client.
func regionServiceClientCtor(a app.App) grpcclients.Client {
	lg := a.Logger().Derive(log.WithFunction("regionServiceClientCtor"))
	clnt, err := regionclient.New(
		app.ServiceClientHostAndPort(a, "regionservice"))
	if err != nil {
		lg.Fatalf("failed to create regionservice client: %v", err)
	}
	return clnt
}

// grpcSearchRegistrar registers the SearchService gRPC server.
func grpcSearchRegistrar(r grpc.ServiceRegistrar, a app.App) {
	urlMap := app.GetAppData[map[string]string](a, AppDataPeliasRegions)

	peliasClients := make(map[string]search.PeliasSearcher, len(urlMap))
	for region, url := range urlMap {
		peliasClients[region] = pelias.New(url)
	}

	regionClient := app.GetServiceClient[*regionclient.Client](a, "regionservice")
	flow := search.NewSearchFlow(peliasClients, regionClient, a.Logger())
	srv := server.NewSearchServer(flow, a.Logger())
	searchv1.RegisterSearchServiceServer(r, srv)
}

// grpcHealthRegistrar registers the HealthService gRPC server.
func grpcHealthRegistrar(r grpc.ServiceRegistrar, a app.App) {
	regionClient := app.GetServiceClient[*regionclient.Client](a, "regionservice")
	probeTTL := time.Duration(app.GetConfigField[int](a.Config(), FldHealthProbeTtlSecs)) * time.Second
	srv := server.NewHealthServer(regionClient, buildPeliasProbers(a), probeTTL, a.Logger())
	healthv1.RegisterHealthServiceServer(r, srv)
}

// buildPeliasProbers returns the configured Pelias clients as health probers,
// ordered by region name for deterministic probing.
func buildPeliasProbers(a app.App) []server.PeliasProber {
	urlMap := app.GetAppData[map[string]string](a, AppDataPeliasRegions)

	regions := make([]string, 0, len(urlMap))
	for region := range urlMap {
		regions = append(regions, region)
	}
	sort.Strings(regions)

	probers := make([]server.PeliasProber, 0, len(regions))
	for _, region := range regions {
		probers = append(probers, pelias.New(urlMap[region]))
	}
	return probers
}

// grpcSearchGateway returns an HTTP handler for the SearchService REST gateway.
func grpcSearchGateway(a app.App) app.ServiceHTTPHandler {
	return func(
		ctx context.Context,
		mux *runtime.ServeMux,
		endpoint string,
		opts []grpc.DialOption,
	) error {
		lg := a.Logger().Derive(log.WithFunction("SearchServiceHTTPHandler"))
		if err := searchv1.RegisterSearchServiceHandlerFromEndpoint(
			ctx, mux, endpoint, opts,
		); err != nil {
			lg.Fatalf("failed to register search gRPC gateway: %v", err)
		}
		return nil
	}
}

// grpcHealthGateway returns an HTTP handler for the HealthService REST gateway.
func grpcHealthGateway(a app.App) app.ServiceHTTPHandler {
	return func(
		ctx context.Context,
		mux *runtime.ServeMux,
		endpoint string,
		opts []grpc.DialOption,
	) error {
		lg := a.Logger().Derive(log.WithFunction("HealthServiceHTTPHandler"))
		if err := healthv1.RegisterHealthServiceHandlerFromEndpoint(
			ctx, mux, endpoint, opts,
		); err != nil {
			lg.Fatalf("failed to register health gRPC gateway: %v", err)
		}
		return nil
	}
}
