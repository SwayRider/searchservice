// Package search implements the search flow and result ranking logic.
package search

import (
	"math"
	"sort"
	"strings"
	"unicode"

	searchv1 "github.com/swayrider/protos/search/v1"
)

const (
	defaultSize           = 5
	maxSize               = 20
	textScoreWeight       = 0.2
	housenumberBonus      = 0.5
	distanceDecayScale    = 5.0 // degrees; half-decay ~500 km
	distanceDecayMax      = 0.5 // maximum penalty cap
	fuzzyStreetPenaltyMax = 1.0 // maximum street mismatch penalty
	streetSimCutoff       = 0.5 // below this similarity, penalty is max
	minConfidence         = 0.0 // drop results below this confidence
	minTokenLen           = 3   // minimum token length for street matching
)

// tokenizeQuery splits s into lowercase tokens of 3+ characters,
// splitting on any non-letter, non-digit rune.
func tokenizeQuery(s string) []string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	tokens := fields[:0]
	for _, f := range fields {
		if len([]rune(f)) >= 3 {
			tokens = append(tokens, f)
		}
	}
	return tokens
}

// queryTextScore returns a bonus in [0, textScoreWeight] based on how many
// query tokens appear as substrings in the result label (case-insensitive).
func queryTextScore(tokens []string, label string) float64 {
	if len(tokens) == 0 {
		return 0
	}
	lower := strings.ToLower(label)
	matched := 0
	for _, t := range tokens {
		if strings.Contains(lower, t) {
			matched++
		}
	}
	return textScoreWeight * float64(matched) / float64(len(tokens))
}

// extractHouseNumbers splits s on non-alphanumeric characters and returns
// tokens that start with a digit (house numbers, postal codes, etc.).
func extractHouseNumbers(s string) []string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	var nums []string
	for _, f := range fields {
		if len(f) > 0 && (f[0] >= '0' && f[0] <= '9') {
			nums = append(nums, f)
		}
	}
	return nums
}

// housenumberMatchScore returns a bonus if the query contains a house number
// token that exactly matches the result's house number.
func housenumberMatchScore(queryNums []string, resultHN string) float64 {
	if len(queryNums) == 0 || resultHN == "" {
		return 0
	}
	hnLower := strings.ToLower(resultHN)
	for _, qn := range queryNums {
		if qn == hnLower {
			return housenumberBonus
		}
	}
	return 0
}

func equirDist(lat, lon, focusLat, focusLon float64) float64 {
	dlat := lat - focusLat
	dlon := (lon - focusLon) * math.Cos(focusLat*math.Pi/180)
	return math.Sqrt(dlat*dlat + dlon*dlon)
}

// distancePenalty returns a score penalty in [0, distanceDecayMax] based on
// equidistant distance from the focus point. Uses exponential decay so nearby
// results are barely affected while distant results are strongly penalized.
func distancePenalty(lat, lon, focusLat, focusLon float64) float64 {
	d := equirDist(lat, lon, focusLat, focusLon)
	return distanceDecayMax * (1 - math.Exp(-d/distanceDecayScale))
}

// editDistance returns the Levenshtein edit distance between a and b.
func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	la, lb := len(ra), len(rb)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			curr[j] = min3(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

// fuzzyStreetPenalty returns a penalty based on the normalized edit distance
// between the longest query token (assumed to be the street name) and the
// result's street. Low similarity → high penalty.
func fuzzyStreetPenalty(queryTokens []string, resultStreet string) float64 {
	if resultStreet == "" {
		return 0
	}
	// Find longest alphabetic token (likely the street name).
	longest := ""
	for _, t := range queryTokens {
		if unicode.IsLetter(rune(t[0])) && len(t) > len(longest) {
			longest = t
		}
	}
	if longest == "" {
		return 0
	}
	streetLower := strings.ToLower(resultStreet)

	// Fast path: exact substring match means no penalty.
	if strings.Contains(streetLower, longest) {
		return 0
	}

	dist := editDistance(longest, streetLower)
	maxLen := max(len(longest), len(streetLower))
	similarity := 1.0 - float64(dist)/float64(maxLen)
	if similarity >= 1.0 {
		return 0
	}
	if similarity <= streetSimCutoff {
		return fuzzyStreetPenaltyMax
	}
	return fuzzyStreetPenaltyMax * (1.0 - similarity) / (1.0 - streetSimCutoff)
}

// Find longest alphabetic token (likely the street name).
// keeping only the best result per group.
// Priority: exact housenumber match > higher confidence > nearer to focus.
// Non-address results are passed through unchanged.
func CollapseAddresses(results []*searchv1.Result, focusLat, focusLon float64) []*searchv1.Result {
	type key struct {
		street   string
		locality string
	}

	// Collect all query house numbers once (from the full result set we can't
	// know the query; this is set by the caller via SetQueryNums before Rank).
	// Instead, we use a simple heuristic: prefer shorter housenumber strings
	// when confidence is equal (exact "8" beats composite "62_8").
	addressMap := make(map[key]*searchv1.Result)
	nonAddress := make([]*searchv1.Result, 0, len(results))

	for _, r := range results {
		if r.Layer != "address" {
			nonAddress = append(nonAddress, r)
			continue
		}
		k := key{street: r.Street, locality: r.Locality}
		existing, ok := addressMap[k]
		if !ok {
			addressMap[k] = r
			continue
		}
		if r.Confidence > existing.Confidence {
			addressMap[k] = r
		} else if r.Confidence == existing.Confidence {
			// Prefer shorter house number (exact "8" over composite "62_8")
			if len(r.Housenumber) < len(existing.Housenumber) {
				addressMap[k] = r
			} else if len(r.Housenumber) == len(existing.Housenumber) {
				if equirDist(r.Lat, r.Lon, focusLat, focusLon) < equirDist(existing.Lat, existing.Lon, focusLat, focusLon) {
					addressMap[k] = r
				}
			}
		}
	}

	collapsed := nonAddress
	for _, r := range addressMap {
		collapsed = append(collapsed, r)
	}
	return collapsed
}

// DeduplicateByID removes duplicate results with the same id (non-empty only).
// When ids match, keeps the result with higher confidence;
// tie on confidence → keep nearest to focusLat/focusLon.
// Results with empty id are passed through unchanged.
func DeduplicateByID(results []*searchv1.Result, focusLat, focusLon float64) []*searchv1.Result {
	byID := make(map[string]*searchv1.Result)
	var noID []*searchv1.Result

	for _, r := range results {
		if r.Id == "" {
			noID = append(noID, r)
			continue
		}
		existing, ok := byID[r.Id]
		if !ok {
			byID[r.Id] = r
			continue
		}
		if r.Confidence > existing.Confidence {
			byID[r.Id] = r
		} else if r.Confidence == existing.Confidence {
			if equirDist(r.Lat, r.Lon, focusLat, focusLon) < equirDist(existing.Lat, existing.Lon, focusLat, focusLon) {
				byID[r.Id] = r
			}
		}
	}

	deduped := noID
	for _, r := range byID {
		deduped = append(deduped, r)
	}
	return deduped
}

// Rank applies collapsing, then deduplication by id, then sorts by
// (confidence + text score + housenumber bonus - distance penalty - street mismatch) DESC / distance ASC,
// then overwrites each result's confidence with the computed score, and truncates to size (default 5, max 20).
func Rank(results []*searchv1.Result, query string, focusLat, focusLon float64, size int) []*searchv1.Result {
	if size <= 0 {
		size = defaultSize
	}
	if size > maxSize {
		size = maxSize
	}

	tokens := tokenizeQuery(query)
	queryNums := extractHouseNumbers(query)
	collapsed := CollapseAddresses(results, focusLat, focusLon)
	deduped := DeduplicateByID(collapsed, focusLat, focusLon)

	sort.Slice(deduped, func(i, j int) bool {
		a, b := deduped[i], deduped[j]
		scoreA := a.Confidence + queryTextScore(tokens, a.Label) + housenumberMatchScore(queryNums, a.Housenumber) - distancePenalty(a.Lat, a.Lon, focusLat, focusLon) - fuzzyStreetPenalty(tokens, a.Street)
		scoreB := b.Confidence + queryTextScore(tokens, b.Label) + housenumberMatchScore(queryNums, b.Housenumber) - distancePenalty(b.Lat, b.Lon, focusLat, focusLon) - fuzzyStreetPenalty(tokens, b.Street)
		if scoreA != scoreB {
			return scoreA > scoreB
		}
		return equirDist(a.Lat, a.Lon, focusLat, focusLon) < equirDist(b.Lat, b.Lon, focusLat, focusLon)
	})

	// Overwrite confidence with the computed ranking score (clamped to [0, 1]).
	for _, r := range deduped {
		r.Confidence = r.Confidence + queryTextScore(tokens, r.Label) + housenumberMatchScore(queryNums, r.Housenumber) - distancePenalty(r.Lat, r.Lon, focusLat, focusLon) - fuzzyStreetPenalty(tokens, r.Street)
		if r.Confidence < 0 {
			r.Confidence = 0
		}
		if r.Confidence > 1 {
			r.Confidence = 1
		}
	}

	// Apply confidence cutoff.
	filtered := deduped[:0]
	for _, r := range deduped {
		if r.Confidence >= minConfidence {
			filtered = append(filtered, r)
		}
	}
	deduped = filtered

	if len(deduped) > size {
		deduped = deduped[:size]
	}

	return deduped
}
