package mealai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"dicho/internal/platform"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

type work struct {
	user, sid, status, token string
	in                       Input
	expires                  time.Time
	attempt                  int
	final                    bool
}

func (s *Service) Handle(ctx context.Context, event platform.Event) error {
	if event.Event != "ai.suggest" {
		return platform.Invalid("Loại tác vụ AI không hợp lệ")
	}
	var payload struct {
		ID string `json:"jobId"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil || !validID(payload.ID) {
		return platform.Invalid("Tác vụ AI không hợp lệ")
	}
	final := event.MaxAttempts > 0 && event.Attempt >= event.MaxAttempts
	err := s.process(ctx, payload.ID, final)
	if final && err != nil {
		var snooze *river.JobSnoozeError
		if !errors.As(err, &snooze) {
			return river.JobSnooze(time.Minute)
		}
	}
	return err
}
func (s *Service) process(ctx context.Context, id string, final bool) error {
	w := work{final: final}
	var raw []byte
	err := s.db.Pool.QueryRow(ctx, "SELECT requested_by::text,space_id::text,status,expires_at,input FROM mealplan.ai_jobs WHERE id=$1", id).Scan(&w.user, &w.sid, &w.status, &w.expires, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if w.status != "pending" {
		return nil
	}
	if !s.db.Clock.Now().Before(w.expires) {
		return s.fail(ctx, id, "", "expired")
	}
	if err = json.Unmarshal(raw, &w.in); err != nil {
		return s.fail(ctx, id, "", "invalid_request")
	}
	claimed := false
	err = s.db.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := s.db.LockMember(ctx, tx, w.sid, w.user); err != nil {
			return err
		}
		var lease *time.Time
		if err := tx.QueryRow(ctx, "SELECT status,attempts,lease_until,expires_at FROM mealplan.ai_jobs WHERE id=$1 FOR UPDATE", id).Scan(&w.status, &w.attempt, &lease, &w.expires); err != nil {
			return err
		}
		if w.status != "pending" {
			return nil
		}
		if !s.db.Clock.Now().Before(w.expires) {
			return nil
		}
		if lease != nil && s.db.Clock.Now().Before(*lease) {
			return river.JobSnooze(lease.Sub(s.db.Clock.Now()))
		}
		if w.attempt >= 2 {
			return nil
		}
		w.token = platform.ID()
		w.attempt++
		if _, err := tx.Exec(ctx, "UPDATE mealplan.ai_jobs SET attempts=$2,attempt_token=$3,lease_until=$4 WHERE id=$1", id, w.attempt, w.token, s.db.Clock.Now().Add(time.Minute)); err != nil {
			return err
		}
		claimed = true
		return nil
	})
	if err != nil {
		if authorizationFailure(err) {
			return s.fail(ctx, id, "", "access_revoked")
		}
		return err
	}
	if !claimed {
		if w.status == "pending" && !s.db.Clock.Now().Before(w.expires) {
			return s.fail(ctx, id, "", "expired")
		}
		if w.status == "pending" && w.attempt >= 2 {
			return s.fail(ctx, id, "", "provider_unavailable")
		}
		return nil
	}
	if s.provider == nil || s.planner == nil {
		return s.providerFailure(ctx, id, w, ErrUnavailable)
	}
	local, err := s.planner.Suggest(ctx, w.user, w.sid, w.in.plannerInput())
	if err != nil {
		if authorizationFailure(err) {
			return s.fail(ctx, id, w.token, "access_revoked")
		}
		var e *platform.Error
		if errors.As(err, &e) && (e.Status == 400 || e.Status == 422) {
			return s.fail(ctx, id, w.token, "invalid_request")
		}
		return s.providerFailure(ctx, id, w, ErrUnavailable)
	}
	prompt := Prompt{Date: w.in.Date, MealSlot: w.in.MealSlot, Servings: w.in.Servings, Limit: w.in.Limit, Candidates: []Candidate{}}
	candidates := make(map[string]string)
	for i, item := range local.Items {
		if i >= 10 {
			break
		}
		key := fmt.Sprintf("r%02d", i+1)
		c := Candidate{Key: key, Name: item.Recipe.Name, Coverage: item.R, Missing: []string{}, Expiring: []string{}}
		for _, food := range item.Missing {
			c.Missing = append(c.Missing, food.Name)
		}
		for _, food := range item.ExpiringUsed {
			c.Expiring = append(c.Expiring, food.Name)
		}
		prompt.Candidates = append(prompt.Candidates, c)
		candidates[key] = item.Recipe.ID
	}
	saved := []storedChoice{}
	if len(prompt.Candidates) > 0 {
		callCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		if !s.db.Clock.Now().Before(w.expires) {
			cancel()
			return s.fail(ctx, id, w.token, "expired")
		}
		choices, callErr := s.provider.Recommend(callCtx, prompt)
		cancel()
		if callErr != nil {
			return s.providerFailure(ctx, id, w, callErr)
		}
		if ValidateChoices(prompt, choices) != nil {
			return s.fail(ctx, id, w.token, "invalid_response")
		}
		for _, choice := range choices {
			saved = append(saved, storedChoice{RecipeID: candidates[choice.Key], Reason: choice.Reason})
		}
	}
	err = s.db.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := s.db.LockMember(ctx, tx, w.sid, w.user); err != nil {
			return err
		}
		var status string
		var token *string
		var expires time.Time
		if err := tx.QueryRow(ctx, "SELECT status,attempt_token::text,expires_at FROM mealplan.ai_jobs WHERE id=$1 FOR UPDATE", id).Scan(&status, &token, &expires); err != nil {
			return err
		}
		if status != "pending" || token == nil || *token != w.token {
			return nil
		}
		if !s.db.Clock.Now().Before(expires) {
			return platform.NotFound()
		}
		data, err := json.Marshal(saved)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "UPDATE mealplan.ai_jobs SET status='ready',result=$2,error_code='',completed_at=$3,attempt_token=NULL,lease_until=NULL WHERE id=$1", id, data, s.db.Clock.Now()); err != nil {
			return err
		}
		return s.db.Enqueue(ctx, tx, "notification", "ai.completed:"+id, map[string]string{"userId": w.user, "title": "Gợi ý món ăn đã sẵn sàng", "body": "Mở gợi ý để xem các món phù hợp với nguyên liệu của bạn.", "type": "activity", "spaceId": w.sid, "entityId": id}, s.db.Clock.Now())
	})
	if authorizationFailure(err) {
		code := "access_revoked"
		if !s.db.Clock.Now().Before(w.expires) {
			code = "expired"
		}
		return s.fail(ctx, id, w.token, code)
	}
	if err != nil {
		return s.providerFailure(ctx, id, w, ErrUnavailable)
	}
	return nil
}
func authorizationFailure(err error) bool {
	var e *platform.Error
	return errors.Is(err, pgx.ErrNoRows) || (errors.As(err, &e) && (e.Status == 401 || e.Status == 403 || e.Status == 404))
}
func (s *Service) fail(ctx context.Context, id, token, code string) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_, err := s.db.Pool.Exec(cleanup, "UPDATE mealplan.ai_jobs SET status='failed',result='[]',error_code=$3,completed_at=$4,attempt_token=NULL,lease_until=NULL WHERE id=$1 AND status='pending' AND ($2='' OR attempt_token=nullif($2,'')::uuid)", id, token, code, s.db.Clock.Now())
	return err
}
func (s *Service) providerFailure(ctx context.Context, id string, w work, cause error) error {
	code := "provider_unavailable"
	if errors.Is(cause, ErrRejected) {
		code = "provider_rejected"
	} else if errors.Is(cause, ErrInvalidResponse) {
		code = "invalid_response"
	}
	if code != "provider_unavailable" || w.attempt >= 2 || w.final {
		return s.fail(ctx, id, w.token, code)
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_, err := s.db.Pool.Exec(cleanup, "UPDATE mealplan.ai_jobs SET error_code=$3,attempt_token=NULL,lease_until=NULL WHERE id=$1 AND status='pending' AND attempt_token=$2", id, w.token, code)
	if err != nil {
		return err
	}
	return ErrUnavailable
}
