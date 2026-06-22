package accessanalysis

import (
	"strings"
)

const RecommendedNxpanelJSON = `log_format nxpanel_json escape=json
  '{"time":"$time_iso8601","ip":"$remote_addr","method":"$request_method",'
  '"uri":"$request_uri","status":$status,"bytes":$body_bytes_sent,'
  '"referer":"$http_referer","ua":"$http_user_agent"}';`

func DetectFormatFromSample(sample string) FormatDetectResponse {
	lines := sampleLines(sample, 20)
	formats := []LogFormat{FormatNxpanelJSON, FormatCombined, FormatCommon}
	best := FormatCombined
	bestOK := -1
	var bestEntries []Entry
	var bestErrors []string
	for _, format := range formats {
		parser, _ := NewParser(string(format), "", false)
		ok := 0
		entries := []Entry{}
		errors := []string{}
		for _, line := range lines {
			entry, err := parser.ParseLine(line)
			if err != nil {
				if len(errors) < 5 {
					errors = append(errors, err.Error())
				}
				continue
			}
			ok++
			if len(entries) < 5 {
				entries = append(entries, entry)
			}
		}
		if ok > bestOK {
			best, bestOK, bestEntries, bestErrors = format, ok, entries, errors
		}
	}
	failureRate := 1.0
	if len(lines) > 0 {
		failureRate = float64(len(lines)-bestOK) / float64(len(lines))
	}
	return FormatDetectResponse{Format: string(best), Parseable: bestOK > 0, FailureRate: failureRate, Samples: bestEntries, RecommendedConf: RecommendedNxpanelJSON, Errors: bestErrors}
}

func TestCustomPattern(pattern, sample string) FormatDetectResponse {
	parser, err := NewParser(string(FormatCustom), pattern, false)
	if err != nil {
		return FormatDetectResponse{Format: string(FormatCustom), Parseable: false, FailureRate: 1, RecommendedConf: RecommendedNxpanelJSON, Errors: []string{err.Error()}}
	}
	lines := sampleLines(sample, 20)
	entries := []Entry{}
	errors := []string{}
	for _, line := range lines {
		entry, err := parser.ParseLine(line)
		if err != nil {
			if len(errors) < 5 {
				errors = append(errors, err.Error())
			}
			continue
		}
		if len(entries) < 5 {
			entries = append(entries, entry)
		}
	}
	failureRate := 1.0
	if len(lines) > 0 {
		failureRate = float64(len(lines)-len(entries)) / float64(len(lines))
	}
	return FormatDetectResponse{Format: string(FormatCustom), Parseable: len(entries) > 0, FailureRate: failureRate, Samples: entries, RecommendedConf: RecommendedNxpanelJSON, Errors: errors}
}

func sampleLines(sample string, max int) []string {
	raw := strings.Split(sample, "\n")
	lines := make([]string, 0, max)
	for _, line := range raw {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lines = append(lines, line)
		if len(lines) >= max {
			break
		}
	}
	return lines
}
