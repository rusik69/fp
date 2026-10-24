package httpapi

import (
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rusik69/fp/internal/airports"
	"github.com/rusik69/fp/internal/airspace"
	"github.com/rusik69/fp/internal/aviation"
	"github.com/rusik69/fp/internal/charts"
	"github.com/rusik69/fp/internal/tfr"
)

//go:embed web/*
var webFiles embed.FS

// NewHandler creates the HTTP API and embedded frontend.
func NewHandler() http.Handler {
	dataDir := os.Getenv("FP_DATA_DIR")
	if dataDir == "" {
		cacheDir, err := os.UserCacheDir()
		if err != nil {
			cacheDir = "data"
		}
		dataDir = filepath.Join(cacheDir, "fp")
	}
	weather := aviation.NewService(nil, filepath.Join(dataDir, "weather"))
	faa := charts.NewFAA(nil, dataDir)
	directory := airports.NewService(nil, filepath.Join(dataDir, "airports.json"))
	airspaces := airspace.NewService(nil, filepath.Join(dataDir, "airspace"))
	return newHandlerWithTFR(weather, faa, directory, airspaces, tfr.NewService(nil, filepath.Join(dataDir, "tfr")))
}

// NewHandlerWithService supports callers that only need the weather service.
func NewHandlerWithService(service *aviation.Service) http.Handler {
	return NewHandlerWithServices(service, nil)
}

// NewHandlerWithServices wires the weather and FAA procedure providers.
func NewHandlerWithServices(service *aviation.Service, faa *charts.FAA) http.Handler {
	return newHandlerWithAirspace(service, faa, nil, airspace.NewService(nil, ""))
}

func newHandler(service *aviation.Service, faa *charts.FAA, directory *airports.Service) http.Handler {
	return newHandlerWithAirspace(service, faa, directory, nil)
}

func newHandlerWithAirspace(service *aviation.Service, faa *charts.FAA, directory *airports.Service, airspaces *airspace.Service) http.Handler {
	return newHandlerWithTFR(service, faa, directory, airspaces, tfr.NewService(nil, ""))
}

func newHandlerWithTFR(service *aviation.Service, faa *charts.FAA, directory *airports.Service, airspaces *airspace.Service, tfrs *tfr.Service) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/v1/airport/{station}", func(w http.ResponseWriter, r *http.Request) {
		station := r.PathValue("station")
		bundle, err := service.Lookup(r.Context(), station)
		if err != nil && directory != nil {
			if airport, fetchedAt, ok := directory.Lookup(station); ok {
				airportJSON, marshalErr := json.Marshal(airport)
				if marshalErr == nil {
					bundle = aviation.Bundle{
						Station:   strings.ToUpper(strings.TrimSpace(station)),
						Source:    "OurAirports public-domain directory (reference only)",
						FetchedAt: fetchedAt,
						Stale:     true,
						Cached:    true,
						Airport:   []json.RawMessage{airportJSON},
					}
					err = nil
				}
			}
		}
		if err != nil {
			status := http.StatusBadGateway
			if errors.Is(err, aviation.ErrInvalidStation) {
				status = http.StatusBadRequest
			}
			writeJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, bundle)
	})
	mux.HandleFunc("POST /api/v1/airports/catalog", func(w http.ResponseWriter, r *http.Request) {
		if directory == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "offline airport directory is unavailable"})
			return
		}
		summary, err := directory.Refresh(r.Context())
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, summary)
	})
	mux.HandleFunc("GET /api/v1/airports/catalog/data", func(w http.ResponseWriter, _ *http.Request) {
		if directory == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "offline airport directory is unavailable"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_, found, err := directory.WriteJSON(w)
		if err != nil {
			return
		}
		if !found {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "airport directory has not been downloaded"})
		}
	})
	mux.HandleFunc("GET /api/v1/sigmets", func(w http.ResponseWriter, r *http.Request) {
		bundle, err := service.SIGMETs(r.Context())
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, bundle)
	})
	if airspaces != nil {
		mux.HandleFunc("GET /api/v1/airspace", func(w http.ResponseWriter, r *http.Request) {
			query := r.URL.Query()
			lat, latErr := strconv.ParseFloat(query.Get("lat"), 64)
			lon, lonErr := strconv.ParseFloat(query.Get("lon"), 64)
			if latErr != nil || lonErr != nil || len(query["lat"]) != 1 || len(query["lon"]) != 1 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": airspace.ErrInvalidCoordinates.Error()})
				return
			}
			result, err := airspaces.Lookup(r.Context(), lat, lon)
			if err != nil {
				status := http.StatusBadGateway
				if errors.Is(err, airspace.ErrInvalidCoordinates) {
					status = http.StatusBadRequest
				}
				writeJSON(w, status, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, result)
		})
	}
	if tfrs != nil {
		mux.HandleFunc("GET /api/v1/tfr", func(w http.ResponseWriter, r *http.Request) {
			result, err := tfrs.Current(r.Context())
			if err != nil {
				writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, result)
		})
	}

	if faa != nil {
		mux.HandleFunc("GET /api/v1/procedures/{station}", func(w http.ResponseWriter, r *http.Request) {
			index, err := faa.Procedures(r.Context(), r.PathValue("station"))
			if err != nil {
				status := http.StatusBadGateway
				if errors.Is(err, charts.ErrInvalidStation) {
					status = http.StatusBadRequest
				}
				writeJSON(w, status, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, index)
		})
		mux.HandleFunc("GET /api/v1/procedures/{station}/files/{filename}", func(w http.ResponseWriter, r *http.Request) {
			path, err := faa.Download(r.Context(), r.PathValue("station"), r.PathValue("filename"))
			if err != nil {
				status := http.StatusBadGateway
				if errors.Is(err, charts.ErrProcedureNotFound) {
					status = http.StatusNotFound
				} else if errors.Is(err, charts.ErrInvalidStation) {
					status = http.StatusBadRequest
				}
				writeJSON(w, status, map[string]string{"error": err.Error()})
				return
			}
			w.Header().Set("Content-Type", "application/pdf")
			w.Header().Set("Content-Disposition", "inline; filename=\""+filepath.Base(path)+"\"")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			http.ServeFile(w, r, path)
		})
		mux.HandleFunc("GET /api/v1/vfr", func(w http.ResponseWriter, r *http.Request) {
			index, err := faa.VFRCharts(r.Context())
			if err != nil {
				writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, index)
		})
		mux.HandleFunc("GET /api/v1/vfr/files/{filename}", func(w http.ResponseWriter, r *http.Request) {
			path, err := faa.DownloadVFR(r.Context(), r.PathValue("filename"))
			if err != nil {
				status := http.StatusBadGateway
				if errors.Is(err, charts.ErrProcedureNotFound) {
					status = http.StatusNotFound
				}
				writeJSON(w, status, map[string]string{"error": err.Error()})
				return
			}
			w.Header().Set("Content-Type", "application/pdf")
			w.Header().Set("Content-Disposition", "inline; filename=\""+filepath.Base(path)+"\"")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			http.ServeFile(w, r, path)
		})
	}
	staticFiles, err := fs.Sub(webFiles, "web")
	if err != nil {
		panic(err)
	}
	staticHandler := http.FileServer(http.FS(staticFiles))
	mux.Handle("GET /", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/manifest.webmanifest" {
			w.Header().Set("Content-Type", "application/manifest+json")
		}
		staticHandler.ServeHTTP(w, r)
	}))
	return mux
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
