package media

import (
	"dicho/internal/platform"
	"net/http"
)

func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /it4788/v1/media/{objectId}", platform.Handle(func(w http.ResponseWriter, r *http.Request) error {
		out, err := s.Read(r.Context(), platform.Current(r).UserID, r.PathValue("objectId"))
		if err != nil {
			return err
		}
		platform.Write(w, 200, out)
		return nil
	}))
	mux.HandleFunc("POST /it4788/v1/media/upload-urls", platform.Handle(func(w http.ResponseWriter, r *http.Request) error {
		var in UploadInput
		if err := platform.Decode(r, &in); err != nil {
			return err
		}
		out, err := s.Upload(r.Context(), platform.Current(r).UserID, in)
		if err != nil {
			return err
		}
		platform.Write(w, 201, out)
		return nil
	}))
	mux.HandleFunc("POST /it4788/v1/media/{objectId}/complete", platform.Handle(func(w http.ResponseWriter, r *http.Request) error {
		if err := s.Complete(r.Context(), platform.Current(r).UserID, r.PathValue("objectId")); err != nil {
			return err
		}
		platform.Write(w, 200, map[string]any{"objectId": r.PathValue("objectId"), "status": "ready"})
		return nil
	}))
}
