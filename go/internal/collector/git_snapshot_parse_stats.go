package collector

import "sort"

type parseLanguageSummary struct {
	Language             string  `json:"language"`
	FileCount            int     `json:"file_count"`
	TotalDurationSeconds float64 `json:"total_duration_seconds"`
	AvgDurationSeconds   float64 `json:"avg_duration_seconds"`
}

type parseLanguageStats struct {
	byLanguage map[string]*parseLanguageSummary
}

func newParseLanguageStats() *parseLanguageStats {
	return &parseLanguageStats{byLanguage: map[string]*parseLanguageSummary{}}
}

func (s *parseLanguageStats) record(language string, durationSeconds float64) {
	if language == "" {
		language = "unknown"
	}
	summary := s.byLanguage[language]
	if summary == nil {
		summary = &parseLanguageSummary{Language: language}
		s.byLanguage[language] = summary
	}
	summary.FileCount++
	summary.TotalDurationSeconds += durationSeconds
	summary.AvgDurationSeconds = summary.TotalDurationSeconds / float64(summary.FileCount)
}

func (s *parseLanguageStats) summaries() []parseLanguageSummary {
	if s == nil || len(s.byLanguage) == 0 {
		return nil
	}
	summaries := make([]parseLanguageSummary, 0, len(s.byLanguage))
	for _, summary := range s.byLanguage {
		summaries = append(summaries, *summary)
	}
	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].TotalDurationSeconds == summaries[j].TotalDurationSeconds {
			return summaries[i].Language < summaries[j].Language
		}
		return summaries[i].TotalDurationSeconds > summaries[j].TotalDurationSeconds
	})
	return summaries
}
