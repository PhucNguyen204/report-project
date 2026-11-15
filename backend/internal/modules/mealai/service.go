package mealai

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"dicho/internal/modules/planning"
	"dicho/internal/platform"
	"github.com/jackc/pgx/v5"
)

type Planner interface {
	Suggest(context.Context, string, string, planning.SuggestionInput) (planning.SuggestionResult, error)
	GetRecipe(context.Context, string, string, string, int) (planning.Recipe, error)
}
type Service struct {
	db       *platform.DB
	planner  Planner
	provider Provider
}

func New(db *platform.DB, planner Planner, provider Provider) *Service {
	return &Service{db, planner, provider}
}

type Input struct {
	Date         string `json:"date,omitempty"`
	MealSlot     string `json:"mealSlot,omitempty"`
	Servings     int    `json:"servings"`
	Limit        int    `json:"limit"`
	ExpiringOnly bool   `json:"expiringOnly,omitempty"`
}
type Item struct {
	Recipe planning.Recipe `json:"recipe"`
	Reason string          `json:"reason"`
}
type Job struct {
	ID        string    `json:"jobId"`
	Status    string    `json:"status"`
	ExpiresAt time.Time `json:"expiresAt"`
	Items     []Item    `json:"items"`
	ErrorCode string    `json:"errorCode,omitempty"`
}
type storedChoice struct {
	RecipeID string `json:"recipeId"`
	Reason   string `json:"reason"`
}

func validID(id string) bool {
	if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		return false
	}
	_, err := hex.DecodeString(strings.ReplaceAll(id, "-", ""))
	return err == nil
}
func (in Input) plannerInput() planning.SuggestionInput {
	return planning.SuggestionInput{Date: in.Date, MealSlot: in.MealSlot, Servings: in.Servings, Limit: in.Limit, ExpiringOnly: in.ExpiringOnly}
}
func (s *Service) normalize(in *Input) error {
	if in.Servings < 1 || in.Servings > 20 || in.Limit < 1 || in.Limit > 10 {
		return platform.Invalid("Khẩu phần từ 1–20 và số gợi ý từ 1–10")
	}
	if in.Date == "" {
		in.Date = s.db.Today().Format("2006-01-02")
	}
	if _, err := time.Parse("2006-01-02", in.Date); err != nil {
		return platform.Invalid("Ngày không hợp lệ")
	}
	if in.MealSlot != "" && in.MealSlot != "breakfast" && in.MealSlot != "lunch" && in.MealSlot != "dinner" {
		return platform.Invalid("Bữa ăn không hợp lệ")
	}
	return nil
}
func (s *Service) Request(ctx context.Context, user, sid string, in Input) (Job, error) {
	if !validID(user) || !validID(sid) {
		return Job{}, platform.NotFound()
	}
	out := Job{ID: platform.ID(), Status: "pending", Items: []Item{}, ExpiresAt: s.db.Clock.Now().Add(24 * time.Hour)}
	err := s.db.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := s.db.LockMember(ctx, tx, sid, user); err != nil {
			return err
		}
		if s.provider == nil || s.planner == nil {
			return platform.Err(503, 5003, "Dịch vụ AI chưa được cấu hình")
		}
		if err := s.normalize(&in); err != nil {
			return err
		}
		if _, err := s.planner.Suggest(platform.WithTransaction(ctx, tx), user, sid, in.plannerInput()); err != nil {
			return err
		}
		now := s.db.Clock.Now()
		var accepted int
		err := tx.QueryRow(ctx, "INSERT INTO mealplan.ai_daily_quotas(user_id,day,accepted) VALUES($1,$2,1) ON CONFLICT(user_id,day) DO UPDATE SET accepted=mealplan.ai_daily_quotas.accepted+1 WHERE mealplan.ai_daily_quotas.accepted<10 RETURNING accepted", user, now.UTC().Format("2006-01-02")).Scan(&accepted)
		if errors.Is(err, pgx.ErrNoRows) {
			return platform.Err(429, 2003, "Đã hết lượt gợi ý AI trong ngày")
		}
		if err != nil {
			return err
		}
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		out.ExpiresAt = now.Add(24 * time.Hour)
		if _, err = tx.Exec(ctx, "INSERT INTO mealplan.ai_jobs(id,space_id,requested_by,input,created_at,expires_at) VALUES($1,$2,$3,$4,$5,$6)", out.ID, sid, user, raw, now, out.ExpiresAt); err != nil {
			return err
		}
		return s.db.Enqueue(ctx, tx, "ai.suggest", out.ID, map[string]string{"jobId": out.ID}, now)
	})
	if err != nil {
		return Job{}, err
	}
	return out, nil
}
func (s *Service) Get(ctx context.Context, user, sid, id string) (Job, error) {
	if !validID(user) || !validID(sid) || !validID(id) {
		return Job{}, platform.NotFound()
	}
	out := Job{Items: []Item{}}
	err := s.db.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := s.db.LockMember(ctx, tx, sid, user); err != nil {
			return err
		}
		var result, input []byte
		err := tx.QueryRow(ctx, "SELECT id::text,status,expires_at,error_code,result,input FROM mealplan.ai_jobs WHERE id=$1 AND space_id=$2 AND requested_by=$3 FOR SHARE", id, sid, user).Scan(&out.ID, &out.Status, &out.ExpiresAt, &out.ErrorCode, &result, &input)
		if errors.Is(err, pgx.ErrNoRows) {
			return platform.NotFound()
		}
		if err != nil {
			return err
		}
		if !s.db.Clock.Now().Before(out.ExpiresAt) {
			return platform.NotFound()
		}
		if out.Status != "ready" {
			return nil
		}
		var saved []storedChoice
		var in Input
		if err = json.Unmarshal(result, &saved); err != nil {
			return err
		}
		if err = json.Unmarshal(input, &in); err != nil {
			return err
		}
		for _, choice := range saved {
			recipe, err := s.planner.GetRecipe(platform.WithTransaction(ctx, tx), user, sid, choice.RecipeID, in.Servings)
			if err != nil {
				var e *platform.Error
				if errors.As(err, &e) && e.Status == 404 {
					continue
				}
				return err
			}
			out.Items = append(out.Items, Item{Recipe: recipe, Reason: choice.Reason})
		}
		return nil
	})
	if err != nil {
		return Job{}, err
	}
	return out, nil
}
func (s *Service) Cleanup(ctx context.Context) error {
	return s.db.Tx(ctx, func(tx pgx.Tx) error {
		now := s.db.Clock.Now()
		if _, err := tx.Exec(ctx, "DELETE FROM mealplan.ai_jobs WHERE expires_at<=$1", now); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "DELETE FROM mealplan.ai_daily_quotas WHERE day<$1", now.UTC().Format("2006-01-02"))
		return err
	})
}
func (s *Service) PurgeUser(ctx context.Context, tx pgx.Tx, user string) error {
	if _, err := tx.Exec(ctx, "DELETE FROM mealplan.ai_jobs WHERE requested_by=$1", user); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, "DELETE FROM mealplan.ai_daily_quotas WHERE user_id=$1", user)
	return err
}
func (s *Service) PurgeSpace(ctx context.Context, tx pgx.Tx, sid string) error {
	_, err := tx.Exec(ctx, "DELETE FROM mealplan.ai_jobs WHERE space_id=$1", sid)
	return err
}
