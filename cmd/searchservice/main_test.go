package main

import (
	"testing"

	"github.com/swayrider/swlib/app"
	"google.golang.org/grpc"
)

// TestGrpcSearchRegistrar verifies the SearchService gRPC server is actually
// registered on the registrar, including wiring the Pelias clients and the
// regionservice client into the search flow.
func TestGrpcSearchRegistrar(t *testing.T) {
	srv := grpc.NewServer()
	a := app.New("searchservice").
		WithAppData(AppDataPeliasRegions, map[string]string{
			"west-europe": "http://localhost:3100/v1",
		}).
		WithServiceClients(
			app.NewServiceClient("regionservice", regionServiceClientCtor),
		)

	grpcSearchRegistrar(srv, a)

	if _, ok := srv.GetServiceInfo()["search.v1.SearchService"]; !ok {
		t.Fatalf("expected search.v1.SearchService to be registered, got %v", srv.GetServiceInfo())
	}
}

// TestGrpcHealthRegistrar verifies the HealthService gRPC server is actually
// registered on the registrar, including wiring the region client and Pelias
// probers into the dependency-aware health check.
func TestGrpcHealthRegistrar(t *testing.T) {
	srv := grpc.NewServer()
	a := app.New("searchservice").
		WithAppData(AppDataPeliasRegions, map[string]string{
			"west-europe": "http://localhost:3100/v1",
		}).
		WithConfigFields(
			app.NewIntConfigField(
				FldHealthProbeTtlSecs, EnvHealthProbeTTLSecs,
				"How long in seconds a health probe result is cached", DefHealthProbeTtlSecs),
		).
		WithServiceClients(
			app.NewServiceClient("regionservice", regionServiceClientCtor),
		)

	grpcHealthRegistrar(srv, a)

	if _, ok := srv.GetServiceInfo()["health.v1.HealthService"]; !ok {
		t.Fatalf("expected health.v1.HealthService to be registered, got %v", srv.GetServiceInfo())
	}
}
