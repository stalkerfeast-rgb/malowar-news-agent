package main

import (
	"math"
	"strings"
	"time"
)

// InterestScore is a discovery aid, NEVER a review or a claim about wishlists.
// Steam's public PopularWishlisted order is an ordinal popularity *proxy*;
// movement between scans is an ordinal momentum proxy, not a growth percentage.
const (
	interestPopularityWeight = 35
	interestMomentumWeight   = 30
	interestRussianWeight    = 25
	interestReleaseWeight    = 10
)

func clampScore(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func strategyMechanics(desc string) []string {
	s := strings.ToLower(plain(desc))
	candidates := []struct {
		name  string
		words []string
	}{
		{"управление ресурсами", []string{"resource management", "manage resources", "resource allocation", "управление ресурсами"}},
		{"строительство и управление базой", []string{"base building", "base-building", "build your base", "base management", "строительство базы"}},
		{"управление поселением/колонией", []string{"colony management", "colony sim", "city building", "city builder", "settlement management", "управление колони", "градостро"}},
		{"управление армией и тактика", []string{"army management", "command armies", "command your army", "tactical combat", "turn-based tactics", "тактические бои", "управление арм"}},
		{"стратегическое планирование", []string{"grand strategy", "real-time strategy", "turn-based strategy", "strategic planning", "strategic decisions", "глобальная стратег", "пошаговая стратег"}},
		{"экономическое управление", []string{"economic management", "manage the economy", "supply chain", "logistics management", "production chains", "торговые пути", "экономическая стратег"}},
	}
	evidence := make([]string, 0, 3)
	for _, c := range candidates {
		for _, w := range c.words {
			if strings.Contains(s, w) {
				evidence = append(evidence, c.name)
				break
			}
		}
	}
	return evidence
}

// Steam strategy genre is sufficient; hybrids require manual editorial approval.
// A hybrid candidate is NOT eligible for auto-release posts until approved.
func strategyEligibility(genres []string, description string) (string, []string) {
	for _, genre := range genres {
		s := strings.ToLower(genre)
		if strings.Contains(s, "strategy") || strings.Contains(s, "стратег") {
			return "strategy", []string{"Steam: жанр Strategy"}
		}
	}
	ev := strategyMechanics(description)
	if len(ev) > 0 {
		return "review", ev
	}
	return "excluded", nil
}

func interestReleaseScore(g ReleaseGame, now time.Time) (int, bool) {
	if !g.ExactDate { // Demo alone is not enough to infer when a release is relevant.
		return 0, false
	}
	days, err := daysUntilMSK(g.Date, now)
	if err != nil {
		return 0, false
	}
	result := 0
	switch {
	case days < -7:
		result = 5
	case days < 0:
		result = 65
	case days == 0:
		result = 100
	case days <= 7:
		result = 95
	case days <= 30:
		result = 75
	case days <= 90:
		result = 45
	default:
		result = 20
	}
	if g.DemoAvailable && result < 90 {
		result = clampScore(result + 10)
	}
	return result, true
}

func scoreInterest(g *ReleaseGame, now time.Time) {
	if g == nil {
		return
	}
	// Ratings do not rank excluded games or unapproved strategy hybrids.
	g.InterestScore = nil
	g.ScoreCoverage = 0
	g.AwardSuggestions = nil
	if g.GenreStatus == "excluded" || g.GenreStatus == "review" {
		return
	}
	weightedSum := 0
	known := 0
	if g.PopularityRank > 0 && g.PopularityPool >= g.PopularityRank {
		total := g.PopularityPool
		popScore := 100
		if total > 1 {
			popScore = clampScore(int(math.Round(100 * float64(total-g.PopularityRank) / float64(total-1))))
		}
		weightedSum += popScore * interestPopularityWeight
		known += interestPopularityWeight
	}
	if g.MomentumKnown {
		momentum := clampScore(50 + 4*g.MomentumRank)
		weightedSum += momentum * interestMomentumWeight
		known += interestMomentumWeight
	}
	switch g.Russian {
	case "yes":
		weightedSum += 100 * interestRussianWeight
		known += interestRussianWeight
	case "not_listed":
		known += interestRussianWeight
	}
	if score, ok := interestReleaseScore(*g, now); ok {
		weightedSum += score * interestReleaseWeight
		known += interestReleaseWeight
	}
	g.ScoreCoverage = known
	// A score without a popularity observation would misrepresent the user's top priority.
	if known < 60 || g.PopularityRank == 0 {
		return
	}
	value := clampScore(int(math.Round(float64(weightedSum) / float64(known))))
	g.InterestScore = &value
	if g.MomentumKnown && g.MomentumRank >= 5 && g.PopularityRank > 15 && value >= 60 {
		g.AwardSuggestions = []string{"Скрытая жемчужина — кандидат: заметный рост позиции при пока невысоком месте в Steam. Нужна редакционная оценка качества игры."}
	}
}

// Always compare measured positions, not hypothetical follower or wishlist counts.
func applyPopularityObservation(g *ReleaseGame, previous ReleaseGame, rank, pool int, at time.Time) {
	if rank <= 0 || pool < rank {
		return
	}
	g.PopularityRank, g.PopularityPool = rank, pool
	g.PopularityObservedAt = at.UTC()
	if previous.PopularityRank > 0 && previous.PopularityObservedAt.Before(at.UTC()) && !previous.PopularityObservedAt.IsZero() {
		g.MomentumRank = previous.PopularityRank - rank
		g.MomentumKnown = true
	} else if previous.MomentumKnown && previous.PopularityRank == rank {
		// Re-checking the same observation must not invent growth or reset a known signal.
		g.MomentumRank = previous.MomentumRank
		g.MomentumKnown = true
	}
}
