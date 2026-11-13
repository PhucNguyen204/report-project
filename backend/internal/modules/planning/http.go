package planning

import (
	"dicho/internal/platform"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strconv"
	"time"
)

func (s *Service) RegisterRecipes(mux *http.ServeMux) {
	base := "/it4788/v1/spaces/{sid}"
	mux.HandleFunc("POST "+base+"/recipes", platform.Handle(func(w http.ResponseWriter, r *http.Request) error {
		var in RecipeInput
		if err := platform.Decode(r, &in); err != nil {
			return err
		}
		out, err := s.CreateRecipe(r.Context(), platform.Current(r).UserID, r.PathValue("sid"), in)
		if err != nil {
			return err
		}
		platform.Write(w, 201, out)
		return nil
	}))
	mux.HandleFunc("GET "+base+"/recipes", platform.Handle(s.listRecipesHTTP))
	mux.HandleFunc("GET "+base+"/recipes/{recipeId}", platform.Handle(s.recipeDetailHTTP))
	mux.HandleFunc("PUT "+base+"/recipes/{recipeId}", platform.Handle(func(w http.ResponseWriter, r *http.Request) error {
		var in RecipeInput
		if err := platform.Decode(r, &in); err != nil {
			return err
		}
		version, err := parseVersion(r.Header.Get("If-Match"))
		if err != nil {
			return err
		}
		out, err := s.UpdateRecipe(r.Context(), platform.Current(r).UserID, r.PathValue("sid"), r.PathValue("recipeId"), version, in)
		if err != nil {
			return err
		}
		platform.Write(w, 200, out)
		return nil
	}))
	mux.HandleFunc("DELETE "+base+"/recipes/{recipeId}", platform.Handle(func(w http.ResponseWriter, r *http.Request) error {
		if err := s.DeleteRecipe(r.Context(), platform.Current(r).UserID, r.PathValue("sid"), r.PathValue("recipeId")); err != nil {
			return err
		}
		platform.Write(w, 200, map[string]bool{"deleted": true})
		return nil
	}))
	mux.HandleFunc("POST "+base+"/recipes/{recipeId}/copy", platform.Handle(func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Name string `json:"name"`
		}
		if err := platform.Decode(r, &in); err != nil {
			return err
		}
		out, err := s.CopyRecipe(r.Context(), platform.Current(r).UserID, r.PathValue("sid"), r.PathValue("recipeId"), in.Name)
		if err != nil {
			return err
		}
		platform.Write(w, 201, out)
		return nil
	}))
	for _, method := range []string{"PUT", "DELETE"} {
		on := method == "PUT"
		mux.HandleFunc(method+" /it4788/v1/me/favorite-recipes/{recipeId}", platform.Handle(func(w http.ResponseWriter, r *http.Request) error {
			if err := s.SetFavorite(r.Context(), platform.Current(r).UserID, r.PathValue("recipeId"), on); err != nil {
				return err
			}
			platform.Write(w, 200, map[string]bool{"favorite": on})
			return nil
		}))
	}
}

func (s *Service) RegisterMeals(mux *http.ServeMux) {
	base := "/it4788/v1/spaces/{sid}"
	mux.HandleFunc("POST "+base+"/meals", platform.Handle(func(w http.ResponseWriter, r *http.Request) error {
		var in MealInput
		if err := platform.Decode(r, &in); err != nil {
			return err
		}
		out, err := s.CreateMeal(r.Context(), platform.Current(r).UserID, r.PathValue("sid"), in)
		if err != nil {
			return err
		}
		platform.Write(w, 201, out)
		return nil
	}))
	mux.HandleFunc("GET "+base+"/meals", platform.Handle(s.listMealsHTTP))
	mux.HandleFunc("PATCH "+base+"/meals/{mealId}", platform.Handle(s.patchMealHTTP))
	mux.HandleFunc("DELETE "+base+"/meals/{mealId}", platform.Handle(func(w http.ResponseWriter, r *http.Request) error {
		if err := s.DeleteMeal(r.Context(), platform.Current(r).UserID, r.PathValue("sid"), r.PathValue("mealId")); err != nil {
			return err
		}
		platform.Write(w, 200, map[string]bool{"deleted": true})
		return nil
	}))
	mux.HandleFunc("POST "+base+"/meals/{mealId}/cook", platform.Handle(func(w http.ResponseWriter, r *http.Request) error {
		var in CookInput
		if err := platform.Decode(r, &in); err != nil {
			return err
		}
		version, err := parseVersion(r.Header.Get("If-Match"))
		if err != nil {
			return platform.Invalid("If-Match phải chứa phiên bản hợp lệ")
		}
		in.Version = version
		out, err := s.Cook(r.Context(), platform.Current(r).UserID, r.PathValue("sid"), r.PathValue("mealId"), in)
		if err != nil {
			return err
		}
		platform.Write(w, 200, out)
		return nil
	}))
}

func (s *Service) listRecipesHTTP(w http.ResponseWriter, r *http.Request) error {
	sid, user := r.PathValue("sid"), platform.Current(r).UserID
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 100 {
			return platform.Invalid("Giới hạn từ 1–100")
		}
		limit = n
	}
	source := r.URL.Query().Get("source")
	if source != "" && source != "space" && source != "template" && source != "favorite" {
		return platform.Invalid("Nguồn công thức không hợp lệ")
	}
	out := []Recipe{}
	err := s.db.Tx(r.Context(), func(tx pgx.Tx) error {
		if _, err := s.db.RequireMember(r.Context(), tx, sid, user); err != nil {
			return err
		}
		rows, err := tx.Query(r.Context(), "SELECT r.id::text FROM recipe.recipes r WHERE r.deleted_at IS NULL AND r.status='published' AND (r.space_id=$1 OR r.kind='template') AND ($2='' OR ($2='space' AND r.space_id=$1) OR ($2='template' AND r.kind='template') OR ($2='favorite' AND EXISTS(SELECT 1 FROM recipe.favorite_recipes f WHERE f.recipe_id=r.id AND f.user_id=$3))) AND ($4='' OR strpos(lower(unaccent(r.name)),lower(unaccent($4)))>0) ORDER BY r.name,r.id LIMIT $5", sid, source, user, r.URL.Query().Get("q"), limit)
		if err != nil {
			return err
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
		for _, id := range ids {
			recipe, err := s.readRecipe(r.Context(), tx, sid, id, 0)
			if err != nil {
				return err
			}
			out = append(out, recipe)
		}
		return nil
	})
	if err != nil {
		return err
	}
	platform.Write(w, 200, map[string]any{"items": out})
	return nil
}
func (s *Service) recipeDetailHTTP(w http.ResponseWriter, r *http.Request) error {
	servings := 0
	if v := r.URL.Query().Get("servings"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 20 {
			return platform.Invalid("Khẩu phần từ 1–20")
		}
		servings = n
	}
	out, err := s.GetRecipe(r.Context(), platform.Current(r).UserID, r.PathValue("sid"), r.PathValue("recipeId"), servings)
	if err != nil {
		return err
	}
	if s.stock != nil {
		if servings == 0 {
			servings = out.Servings
		}
		out.Comparison, err = s.Compare(r.Context(), platform.Current(r).UserID, r.PathValue("sid"), out.ID, servings)
		if err != nil {
			return err
		}
	}
	w.Header().Set("ETag", strconv.FormatInt(out.Version, 10))
	platform.Write(w, 200, out)
	return nil
}
func (s *Service) listMealsHTTP(w http.ResponseWriter, r *http.Request) error {
	from, to := s.db.Today(), s.db.Today().AddDate(0, 0, 6)
	var err error
	if v := r.URL.Query().Get("from"); v != "" {
		from, err = parseDate(v)
		if err != nil {
			return err
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		to, err = parseDate(v)
		if err != nil {
			return err
		}
	}
	if dateUTC(to).Before(dateUTC(from)) || dateUTC(to).Sub(dateUTC(from)) > 366*24*time.Hour {
		return platform.Invalid("Khoảng ngày không hợp lệ")
	}
	out := []Meal{}
	err = s.db.Tx(r.Context(), func(tx pgx.Tx) error {
		if _, err := s.db.RequireMember(r.Context(), tx, r.PathValue("sid"), platform.Current(r).UserID); err != nil {
			return err
		}
		rows, err := tx.Query(r.Context(), "SELECT id::text,recipe_id::text,name,meal_date::text,meal_slot,servings,status,version FROM mealplan.meal_entries WHERE space_id=$1 AND meal_date BETWEEN $2 AND $3 AND deleted_at IS NULL ORDER BY meal_date,meal_slot,id", r.PathValue("sid"), from, to)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var m Meal
			if err = rows.Scan(&m.ID, &m.RecipeID, &m.Name, &m.Date, &m.MealSlot, &m.Servings, &m.Status, &m.Version); err != nil {
				return err
			}
			out = append(out, m)
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	platform.Write(w, 200, map[string]any{"items": out})
	return nil
}
func (s *Service) patchMealHTTP(w http.ResponseWriter, r *http.Request) error {
	var patch struct {
		RecipeID *string `json:"recipeId"`
		Name     *string `json:"name"`
		Date     *string `json:"date"`
		MealSlot *string `json:"mealSlot"`
		Servings *int    `json:"servings"`
	}
	if err := platform.Decode(r, &patch); err != nil {
		return err
	}
	version, err := parseVersion(r.Header.Get("If-Match"))
	if err != nil {
		return err
	}
	var in MealInput
	err = s.db.Tx(r.Context(), func(tx pgx.Tx) error {
		if _, err := s.db.RequireMember(r.Context(), tx, r.PathValue("sid"), platform.Current(r).UserID); err != nil {
			return err
		}
		m, err := readMeal(r.Context(), tx, r.PathValue("sid"), r.PathValue("mealId"), false)
		if err != nil {
			return err
		}
		in = MealInput{Name: m.Name, Date: m.Date, MealSlot: m.MealSlot, Servings: m.Servings}
		if m.RecipeID != nil {
			in.RecipeID = *m.RecipeID
		}
		return nil
	})
	if err != nil {
		return err
	}
	if patch.RecipeID != nil {
		in.RecipeID = *patch.RecipeID
	}
	if patch.Name != nil {
		in.Name = *patch.Name
	}
	if patch.Date != nil {
		in.Date = *patch.Date
	}
	if patch.MealSlot != nil {
		in.MealSlot = *patch.MealSlot
	}
	if patch.Servings != nil {
		in.Servings = *patch.Servings
	}
	out, err := s.UpdateMeal(r.Context(), platform.Current(r).UserID, r.PathValue("sid"), r.PathValue("mealId"), version, in)
	if err != nil {
		return err
	}
	platform.Write(w, 200, out)
	return nil
}
