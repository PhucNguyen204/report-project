package planning

import (
	"dicho/internal/platform"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strconv"
)

func (s *Service) Register(mux *http.ServeMux) {
	s.RegisterRecipes(mux)
	s.RegisterMeals(mux)
	s.RegisterMissing(mux)
	s.RegisterSuggestions(mux)
	s.RegisterReports(mux)
	s.RegisterSearch(mux)
	s.RegisterTemplates(mux)
}
func (s *Service) RegisterMissing(mux *http.ServeMux) {
	mux.HandleFunc("POST /it4788/v1/spaces/{sid}/shopping-lists/generate", platform.Handle(func(w http.ResponseWriter, r *http.Request) error {
		var in GenerateInput
		if err := platform.Decode(r, &in); err != nil {
			return err
		}
		out, err := s.Generate(r.Context(), platform.Current(r).UserID, r.PathValue("sid"), in)
		if err != nil {
			return err
		}
		status := 201
		if in.DryRun {
			status = 200
		}
		platform.Write(w, status, out)
		return nil
	}))
	mux.HandleFunc("POST /it4788/v1/spaces/{sid}/recipes/{recipeId}/missing-to-list", platform.Handle(func(w http.ResponseWriter, r *http.Request) error {
		var in RecipeMissingInput
		if err := platform.Decode(r, &in); err != nil {
			return err
		}
		out, err := s.MissingRecipe(r.Context(), platform.Current(r).UserID, r.PathValue("sid"), r.PathValue("recipeId"), in)
		if err != nil {
			return err
		}
		status := 201
		if in.DryRun {
			status = 200
		}
		platform.Write(w, status, out)
		return nil
	}))
}
func (s *Service) RegisterSuggestions(mux *http.ServeMux) {
	mux.HandleFunc("GET /it4788/v1/spaces/{sid}/recipe-suggestions", platform.Handle(func(w http.ResponseWriter, r *http.Request) error {
		in := SuggestionInput{Date: r.URL.Query().Get("date"), MealSlot: r.URL.Query().Get("mealSlot")}
		var err error
		if v := r.URL.Query().Get("servings"); v != "" {
			in.Servings, err = strconv.Atoi(v)
			if err != nil || in.Servings < 1 || in.Servings > 20 {
				return platform.Invalid("Khẩu phần từ 1–20")
			}
		}
		if v := r.URL.Query().Get("limit"); v != "" {
			in.Limit, err = strconv.Atoi(v)
			if err != nil || in.Limit < 1 {
				return platform.Invalid("Giới hạn không hợp lệ")
			}
		}
		if v := r.URL.Query().Get("expiringOnly"); v != "" {
			in.ExpiringOnly, err = strconv.ParseBool(v)
			if err != nil {
				return platform.Invalid("Bộ lọc không hợp lệ")
			}
		}
		out, err := s.Suggest(r.Context(), platform.Current(r).UserID, r.PathValue("sid"), in)
		if err != nil {
			return err
		}
		platform.Write(w, 200, out)
		return nil
	}))
}
func (s *Service) RegisterReports(mux *http.ServeMux) {
	for _, kind := range []string{"spending", "consumption", "waste"} {
		mux.HandleFunc("GET /it4788/v1/spaces/{sid}/reports/"+kind, platform.Handle(func(w http.ResponseWriter, r *http.Request) error {
			to := s.db.Today()
			from := to.AddDate(0, 0, 1-to.Day())
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
			group := r.URL.Query().Get("groupBy")
			if group == "" {
				group = "day"
			}
			out, err := s.Report(r.Context(), platform.Current(r).UserID, r.PathValue("sid"), kind, from, to, group)
			if err != nil {
				return err
			}
			platform.Write(w, 200, out)
			return nil
		}))
	}
}
func (s *Service) RegisterSearch(mux *http.ServeMux) {
	for _, spec := range []struct {
		path  string
		foods bool
	}{{"search", false}, {"foods/suggest", true}} {
		mux.HandleFunc("GET /it4788/v1/spaces/{sid}/"+spec.path, platform.Handle(func(w http.ResponseWriter, r *http.Request) error {
			limit := 50
			if spec.foods {
				limit = 10
			}
			if v := r.URL.Query().Get("limit"); v != "" {
				n, err := strconv.Atoi(v)
				if err != nil {
					return platform.Invalid("Giới hạn không hợp lệ")
				}
				limit = n
			}
			out, err := s.Search(r.Context(), platform.Current(r).UserID, r.PathValue("sid"), r.URL.Query().Get("q"), limit, spec.foods)
			if err != nil {
				return err
			}
			platform.Write(w, 200, out)
			return nil
		}))
	}
}
func (s *Service) RegisterTemplates(mux *http.ServeMux) {
	base := "/it4788/v1/admin/recipe-templates"
	mux.HandleFunc("POST "+base, platform.Handle(func(w http.ResponseWriter, r *http.Request) error {
		if err := platform.Admin(r); err != nil {
			return err
		}
		var in TemplateInput
		if err := platform.Decode(r, &in); err != nil {
			return err
		}
		out, err := s.SaveTemplate(r.Context(), platform.Current(r).UserID, "", 0, in)
		if err != nil {
			return err
		}
		platform.Write(w, 201, out)
		return nil
	}))
	mux.HandleFunc("PUT "+base+"/{id}", platform.Handle(func(w http.ResponseWriter, r *http.Request) error {
		if err := platform.Admin(r); err != nil {
			return err
		}
		var in TemplateInput
		if err := platform.Decode(r, &in); err != nil {
			return err
		}
		version, err := parseVersion(r.Header.Get("If-Match"))
		if err != nil {
			return err
		}
		out, err := s.SaveTemplate(r.Context(), platform.Current(r).UserID, r.PathValue("id"), version, in)
		if err != nil {
			return err
		}
		platform.Write(w, 200, out)
		return nil
	}))
	mux.HandleFunc("DELETE "+base+"/{id}", platform.Handle(func(w http.ResponseWriter, r *http.Request) error {
		if err := platform.Admin(r); err != nil {
			return err
		}
		if err := s.DeleteTemplate(r.Context(), platform.Current(r).UserID, r.PathValue("id")); err != nil {
			return err
		}
		platform.Write(w, 200, map[string]bool{"deleted": true})
		return nil
	}))
	mux.HandleFunc("GET "+base, platform.Handle(func(w http.ResponseWriter, r *http.Request) error {
		if err := platform.Admin(r); err != nil {
			return err
		}
		page, err := platform.ReadPage(r, platform.PageText, platform.PageUUID)
		if err != nil {
			return err
		}
		out := []Recipe{}
		next := ""
		err = s.db.Tx(r.Context(), func(tx pgx.Tx) error {
			if err := requireAdmin(r.Context(), tx, platform.Current(r).UserID); err != nil {
				return err
			}
			rows, err := tx.Query(r.Context(), `SELECT id::text,name FROM recipe.recipes WHERE kind='template' AND deleted_at IS NULL AND ($1='' OR (name,id)>($2,NULLIF($1,'')::uuid)) ORDER BY name,id LIMIT $3`, page.After[1], page.After[0], page.Limit+1)
			if err != nil {
				return err
			}
			type templateKey struct{ id, name string }
			keys := []templateKey{}
			for rows.Next() {
				var key templateKey
				if err = rows.Scan(&key.id, &key.name); err != nil {
					rows.Close()
					return err
				}
				keys = append(keys, key)
			}
			rows.Close()
			if err = rows.Err(); err != nil {
				return err
			}
			if len(keys) > page.Limit {
				last := keys[page.Limit-1]
				next = page.Next(last.name, last.id)
				keys = keys[:page.Limit]
			}
			for _, key := range keys {
				item, err := s.readTemplate(r.Context(), tx, key.id)
				if err != nil {
					return err
				}
				out = append(out, item)
			}
			return nil
		})
		if err != nil {
			return err
		}
		platform.WritePage(w, map[string]any{"items": out}, next, s.db.Clock.Now())
		return nil
	}))
}
