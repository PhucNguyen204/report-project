package mealai

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrUnavailable     = errors.New("AI provider unavailable")
	ErrRejected        = errors.New("AI request rejected")
	ErrInvalidResponse = errors.New("AI provider response invalid")
)

type Candidate struct {
	Key      string   `json:"key"`
	Name     string   `json:"name"`
	Coverage float64  `json:"coverage"`
	Missing  []string `json:"missing"`
	Expiring []string `json:"expiring"`
}
type Prompt struct {
	Date       string      `json:"date"`
	MealSlot   string      `json:"mealSlot"`
	Servings   int         `json:"servings"`
	Limit      int         `json:"limit"`
	Candidates []Candidate `json:"candidates"`
}
type Choice struct {
	Key    string `json:"key"`
	Reason string `json:"reason"`
}
type Provider interface {
	Recommend(context.Context, Prompt) ([]Choice, error)
}

func ValidateChoices(prompt Prompt, choices []Choice) error {
	if validatePrompt(prompt) != nil || len(choices) == 0 || len(choices) > prompt.Limit || len(choices) > len(prompt.Candidates) {
		return ErrInvalidResponse
	}
	allowed := make(map[string]bool, len(prompt.Candidates))
	for _, candidate := range prompt.Candidates {
		allowed[candidate.Key] = true
	}
	seen := make(map[string]bool, len(choices))
	for _, choice := range choices {
		if !allowed[choice.Key] || seen[choice.Key] || !boundedText(choice.Reason, 500) {
			return ErrInvalidResponse
		}
		seen[choice.Key] = true
	}
	return nil
}
func validatePrompt(prompt Prompt) error {
	if _, err := time.Parse("2006-01-02", prompt.Date); err != nil {
		return ErrRejected
	}
	if prompt.MealSlot != "" && prompt.MealSlot != "breakfast" && prompt.MealSlot != "lunch" && prompt.MealSlot != "dinner" {
		return ErrRejected
	}
	if prompt.Servings < 1 || prompt.Servings > 20 || prompt.Limit < 1 || prompt.Limit > 10 || len(prompt.Candidates) < 1 || len(prompt.Candidates) > 10 {
		return ErrRejected
	}
	seen := make(map[string]bool, len(prompt.Candidates))
	for _, candidate := range prompt.Candidates {
		if len(candidate.Key) != 3 || candidate.Key[0] != 'r' || candidate.Key[1] < '0' || candidate.Key[1] > '9' || candidate.Key[2] < '0' || candidate.Key[2] > '9' || candidate.Key < "r01" || candidate.Key > "r10" || seen[candidate.Key] || !boundedText(candidate.Name, 200) || math.IsNaN(candidate.Coverage) || math.IsInf(candidate.Coverage, 0) || candidate.Coverage < 0 || candidate.Coverage > 1 {
			return ErrRejected
		}
		seen[candidate.Key] = true
		for _, names := range [][]string{candidate.Missing, candidate.Expiring} {
			if len(names) > 50 {
				return ErrRejected
			}
			for _, name := range names {
				if !boundedText(name, 200) {
					return ErrRejected
				}
			}
		}
	}
	return nil
}

func boundedText(value string, maxRunes int) bool {
	return utf8.ValidString(value) && strings.TrimSpace(value) != "" && utf8.RuneCountInString(value) <= maxRunes
}
