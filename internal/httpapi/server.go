package httpapi

import (
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rusik69/fp/internal/airports"
	"github.com/rusik69/fp/internal/airspace"
	"github.com/rusik69/fp/internal/aviation"
	"github.com/rusik69/fp/internal/charts"
	"github.com/rusik69/fp/internal/ibonav"
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
	return newHandlerWithIbonav(weather, faa, directory, airspaces, tfr.NewService(nil, filepath.Join(dataDir, "tfr")), ibonav.NewClient(os.Getenv("FP_IBONAV_PERSONAL_TOKEN")))
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
	return newHandlerWithIbonav(service, faa, directory, airspaces, tfrs, nil)
}

func newHandlerWithIbonav(service *aviation.Service, faa *charts.FAA, directory *airports.Service, airspaces *airspace.Service, tfrs *tfr.Service, ibonavClient *ibonav.Client) http.Handler {
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
	mux.HandleFunc("GET /api/v1/ibonav/status", func(w http.ResponseWriter, _ *http.Request) {
		enabled := ibonavClient != nil && ibonavClient.Enabled()
		writeJSON(w, http.StatusOK, map[string]any{
			"provider":      "ibonav.aero",
			"enabled":       enabled,
			"roles":         []string{"AIS Map Frame Access", "AIS PIB API", "AIS Charts API"},
			"configuration": "Set FP_IBONAV_PERSONAL_TOKEN on the server after the provider grants the required developer roles.",
		})
	})
	mux.HandleFunc("GET /api/v1/ibonav/map-token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if ibonavClient == nil || !ibonavClient.Enabled() {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "iBonav map access is not configured on this server."})
			return
		}
		renew := r.URL.Query().Get("renew") == "true"
		token, expiresIn, err := ibonavClient.AccessToken(r.Context(), renew)
		if err != nil {
			writeIbonavError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"token": token, "expires_in": expiresIn, "roles": []string{}})
	})
	mux.HandleFunc("GET /api/v1/ibonav/notams/availability", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if ibonavClient == nil || !ibonavClient.Enabled() {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "iBonav NOTAM access is not configured on this server."})
			return
		}
		data, err := ibonavClient.NotamAvailability(r.Context())
		if err != nil {
			writeIbonavError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"source":    "ibonav.aero AIS PIB API",
			"fetchedAt": time.Now().UTC(),
			"data":      data,
		})
	})
	mux.HandleFunc("GET /api/v1/ibonav/notams", func(w http.ResponseWriter, r *http.Request) {
		query, err := validateIbonavNotamQuery(r.URL.Query())
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if ibonavClient == nil || !ibonavClient.Enabled() {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "iBonav NOTAM access is not configured on this server."})
			return
		}
		data, err := ibonavClient.SearchNotams(r.Context(), query)
		if err != nil {
			writeIbonavError(w, err)
			return
		}
		var result struct {
			Total  int               `json:"total"`
			Notams []json.RawMessage `json:"notams"`
		}
		if err := json.Unmarshal(data, &result); err != nil || result.Notams == nil || result.Total < 0 {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "iBonav NOTAM API returned an invalid search result."})
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, map[string]any{
			"source":    "ibonav.aero AIS PIB API",
			"fetchedAt": time.Now().UTC(),
			"total":     result.Total,
			"data":      data,
		})
	})

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
func validateIbonavNotamQuery(input url.Values) (url.Values, error) {
	for key := range input {
		switch key {
		case "location", "validFrom", "validTo", "limit", "offset":
		default:
			return nil, errors.New("unsupported NOTAM search parameter")
		}
		if len(input[key]) != 1 {
			return nil, errors.New("each NOTAM search parameter must appear once")
		}
	}
	location := strings.ToUpper(strings.TrimSpace(input.Get("location")))
	codes := strings.Split(location, ",")
	if location == "" || len(codes) > 10 {
		return nil, errors.New("enter 1–10 comma-separated airport or FIR identifiers")
	}
	normalizedCodes := make([]string, 0, len(codes))
	for _, code := range codes {
		code = strings.TrimSpace(code)
		normalizedCodes = append(normalizedCodes, code)
		if len(code) < 3 || len(code) > 8 {
			return nil, errors.New("each airport or FIR identifier must be 3–8 letters, digits, or ? wildcards")
		}
		for _, character := range code {
			if !((character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '?') {
				return nil, errors.New("each airport or FIR identifier must be 3–8 letters, digits, or ? wildcards")
			}
		}
	}
	location = strings.Join(normalizedCodes, ",")
	from, err := time.Parse(time.RFC3339, input.Get("validFrom"))
	if err != nil {
		return nil, errors.New("validFrom must be an RFC3339 UTC timestamp")
	}
	to, err := time.Parse(time.RFC3339, input.Get("validTo"))
	if err != nil || !to.After(from) || to.Sub(from) > 7*24*time.Hour {
		return nil, errors.New("validTo must follow validFrom and the interval cannot exceed seven days")
	}
	limit := 200
	if value := input.Get("limit"); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 500 {
			return nil, errors.New("limit must be between 1 and 500")
		}
	}
	offset := 0
	if value := input.Get("offset"); value != "" {
		offset, err = strconv.Atoi(value)
		if err != nil || offset < 0 || offset > 10000 {
			return nil, errors.New("offset must be between 0 and 10000")
		}
	}
	return url.Values{
		"location":  {location},
		"validFrom": {from.UTC().Format(time.RFC3339)},
		"validTo":   {to.UTC().Format(time.RFC3339)},
		"limit":     {strconv.Itoa(limit)},
		"offset":    {strconv.Itoa(offset)},
	}, nil
}

func writeIbonavError(w http.ResponseWriter, err error) {
	var apiError *ibonav.APIError
	if errors.As(err, &apiError) {
		if apiError.StatusCode == http.StatusTooManyRequests {
			if apiError.RetryAfter != "" && len(apiError.RetryAfter) <= 16 {
				w.Header().Set("Retry-After", apiError.RetryAfter)
			}
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "iBonav rate limit reached; retry after the indicated delay."})
			return
		}
		if apiError.StatusCode == http.StatusUnauthorized || apiError.StatusCode == http.StatusForbidden {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "iBonav denied access; verify the server token and assigned AIS PIB, map-frame, and charts roles."})
			return
		}
	}
	writeJSON(w, http.StatusBadGateway, map[string]string{"error": "iBonav service is unavailable or returned an invalid response."})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
