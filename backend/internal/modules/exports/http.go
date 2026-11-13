package exports

import (
	"dicho/internal/platform"
	"net/http"
)

func (s *Service) Register(mux *http.ServeMux) {
	base := "/it4788/v1/spaces/{sid}/reports/exports"
	mux.HandleFunc("POST "+base, platform.Handle(func(w http.ResponseWriter, r *http.Request) error {
		var in Input
		if err := platform.Decode(r, &in); err != nil {
			return err
		}
		out, err := s.Request(r.Context(), platform.Current(r).UserID, r.PathValue("sid"), in)
		if err != nil {
			return err
		}
		platform.Write(w, 202, out)
		return nil
	}))
	mux.HandleFunc("GET "+base+"/{exportId}", platform.Handle(func(w http.ResponseWriter, r *http.Request) error {
		out, err := s.Get(r.Context(), platform.Current(r).UserID, r.PathValue("sid"), r.PathValue("exportId"))
		if err != nil {
			return err
		}
		platform.Write(w, 200, out)
		return nil
	}))
}
