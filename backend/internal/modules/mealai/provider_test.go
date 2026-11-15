package mealai

import (
	"errors"
	"math"
	"strings"
	"testing"
)

func validPrompt() Prompt {
	return Prompt{Date: "2026-11-14", MealSlot: "dinner", Servings: 2, Limit: 2, Candidates: []Candidate{
		{Key: "r01", Name: "Canh cải", Coverage: 1, Expiring: []string{"Cải xanh"}},
		{Key: "r02", Name: "Cơm trứng", Coverage: .5, Missing: []string{"Trứng"}},
	}}
}

func TestValidateChoicesRejectsInventedOrDuplicateRecipes(t *testing.T) {
	cases := []struct {
		name    string
		choices []Choice
	}{
		{"nil", nil}, {"empty", []Choice{}},
		{"invented", []Choice{{Key: "r99", Reason: "Ngon"}}},
		{"missing key", []Choice{{Reason: "Ngon"}}},
		{"duplicate", []Choice{{Key: "r01", Reason: "Ngon"}, {Key: "r01", Reason: "Đủ đồ"}}},
		{"empty reason", []Choice{{Key: "r01"}}},
		{"blank reason", []Choice{{Key: "r01", Reason: " \n\t "}}},
		{"long reason", []Choice{{Key: "r01", Reason: strings.Repeat("đ", 501)}}},
		{"too many", []Choice{{Key: "r01", Reason: "A"}, {Key: "r02", Reason: "B"}, {Key: "r03", Reason: "C"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateChoices(validPrompt(), tc.choices); !errors.Is(err, ErrInvalidResponse) {
				t.Fatalf("got %v, want invalid response", err)
			}
		})
	}
	if err := ValidateChoices(validPrompt(), []Choice{{Key: "r02", Reason: strings.Repeat("đ", 500)}, {Key: "r01", Reason: "Ưu tiên rau sắp hết hạn"}}); err != nil {
		t.Fatal(err)
	}
	p := validPrompt()
	p.Limit = 1
	if err := ValidateChoices(p, []Choice{{Key: "r01", Reason: "A"}, {Key: "r02", Reason: "B"}}); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("limit ignored: %v", err)
	}
}

func TestPromptBoundsRejectUnsafeProviderInput(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Prompt)
	}{
		{"date", func(p *Prompt) { p.Date = "2026-02-30" }},
		{"date absent", func(p *Prompt) { p.Date = "" }},
		{"slot", func(p *Prompt) { p.MealSlot = "midnight" }},
		{"servings zero", func(p *Prompt) { p.Servings = 0 }},
		{"servings high", func(p *Prompt) { p.Servings = 21 }},
		{"limit zero", func(p *Prompt) { p.Limit = 0 }},
		{"limit high", func(p *Prompt) { p.Limit = 11 }},
		{"no candidates", func(p *Prompt) { p.Candidates = nil }},
		{"too many candidates", func(p *Prompt) { p.Candidates = make([]Candidate, 11) }},
		{"alias", func(p *Prompt) { p.Candidates[0].Key = "recipe-id" }},
		{"punctuation alias", func(p *Prompt) { p.Candidates[0].Key = "r0:" }},
		{"duplicate alias", func(p *Prompt) { p.Candidates[1].Key = "r01" }},
		{"empty name", func(p *Prompt) { p.Candidates[0].Name = " " }},
		{"long name", func(p *Prompt) { p.Candidates[0].Name = strings.Repeat("đ", 201) }},
		{"nan", func(p *Prompt) { p.Candidates[0].Coverage = math.NaN() }},
		{"infinite", func(p *Prompt) { p.Candidates[0].Coverage = math.Inf(1) }},
		{"negative coverage", func(p *Prompt) { p.Candidates[0].Coverage = -.01 }},
		{"high coverage", func(p *Prompt) { p.Candidates[0].Coverage = 1.01 }},
		{"many missing", func(p *Prompt) { p.Candidates[0].Missing = make([]string, 51) }},
		{"long missing", func(p *Prompt) { p.Candidates[0].Missing = []string{strings.Repeat("đ", 201)} }},
		{"blank expiring", func(p *Prompt) { p.Candidates[0].Expiring = []string{" "} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := validPrompt()
			tc.change(&p)
			if err := validatePrompt(p); !errors.Is(err, ErrRejected) {
				t.Fatalf("got %v, want rejected", err)
			}
		})
	}
	p := validPrompt()
	p.MealSlot = ""
	p.Servings = 20
	p.Limit = 10
	p.Candidates[0].Name = strings.Repeat("đ", 200)
	if err := validatePrompt(p); err != nil {
		t.Fatal(err)
	}
}
