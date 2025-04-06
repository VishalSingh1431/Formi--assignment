package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/kellydunn/golang-geo"
	"github.com/agnivade/levenshtein"
)

type Property struct {
	Name      string  `json:"name"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type PropertyResponse struct {
	Name      string  `json:"name"`
	Distance  float64 `json:"distance_km"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type SearchResponse struct {
	Properties []PropertyResponse `json:"properties"`
	Message    string             `json:"message,omitempty"`
}

var properties = []Property{
	{"Moustache Udaipur Luxuria", 24.57799888, 73.68263271},
	{"Moustache Udaipur", 24.58145726, 73.68223671},
	{"Moustache Udaipur Verandah", 24.58350565, 73.68120777},
	{"Moustache Jaipur", 27.29124839, 75.89630143},
	{"Moustache Jaisalmer", 27.20578572, 70.85906998},
	{"Moustache Jodhpur", 26.30365556, 73.03570908},
	{"Moustache Agra", 27.26156953, 78.07524716},
	{"Moustache Delhi", 28.61257139, 77.28423582},
	{"Moustache Rishikesh Luxuria", 30.13769036, 78.32465767},
	{"Moustache Rishikesh Riverside Resort", 30.10216117, 78.38458848},
	{"Moustache Hostel Varanasi", 25.2992622, 82.99691388},
	{"Moustache Goa Luxuria", 15.6135195, 73.75705228},
	{"Moustache Koksar Luxuria", 32.4357785, 77.18518717},
	{"Moustache Daman", 20.41486263, 72.83282455},
	{"Panarpani Retreat", 22.52805539, 78.43116291},
	{"Moustache Pushkar", 26.48080513, 74.5613783},
	{"Moustache Khajuraho", 24.84602104, 79.93139381},
	{"Moustache Manali", 32.28818695, 77.17702523},
	{"Moustache Bhintal Luxuria", 29.36552248, 79.53481747},
	{"Moustache Srinagar", 34.11547314, 74.88701741},
	{"Moustache Ranthambore Luxuria", 26.05471373, 76.42953726},
	{"Moustache Coimbatore", 11.02064612, 76.96293531},
	{"Moustache Shoja", 31.56341267, 77.36733331},
}

var cityCenters = map[string]struct {
	Lat float64
	Lon float64
}{
	"udaipur":    {24.5854, 73.7125},
	"jaipur":     {26.9124, 75.7873},
	"jaisalmer":  {26.9157, 70.9083},
	"delih":      {28.7041, 77.1025},
	"udiapur":    {24.5854, 73.7125},
}

var (
	searchCache = make(map[string]SearchResponse)
	cacheMutex  sync.RWMutex
)

func calculateDistance(lat1, lon1, lat2, lon2 float64) float64 {
	p1 := geo.NewPoint(lat1, lon1)
	p2 := geo.NewPoint(lat2, lon2)
	return p1.GreatCircleDistance(p2)
}

func findBestCityMatch(query string) string {
	query = strings.ToLower(query)
	var bestMatch string
	minDistance := 2
	for city := range cityCenters {
		distance := levenshtein.ComputeDistance(query, city)
		if distance < minDistance {
			minDistance = distance
			bestMatch = city
		}
	}
	return bestMatch
}

func searchProperties(query string) SearchResponse {
	startTime := time.Now()
	query = strings.TrimSpace(query)

	cacheKey := strings.ToLower(query)
	cacheMutex.RLock()
	if cached, exists := searchCache[cacheKey]; exists {
		cacheMutex.RUnlock()
		log.Printf("Cache hit for: %s", query)
		return cached
	}
	cacheMutex.RUnlock()

	var targetLat, targetLon float64
	var found bool

	if coords, exists := cityCenters[strings.ToLower(query)]; exists {
		targetLat, targetLon = coords.Lat, coords.Lon
		found = true
	} else {
		bestMatch := findBestCityMatch(query)
		if bestMatch != "" {
			log.Printf("Fuzzy matched '%s' to '%s'", query, bestMatch)
			targetLat, targetLon = cityCenters[bestMatch].Lat, cityCenters[bestMatch].Lon
			found = true
			cacheKey = bestMatch
		}
	}

	if !found {
		response := SearchResponse{
			Properties: []PropertyResponse{},
			Message:    "Location not recognized",
		}
		cacheMutex.Lock()
		searchCache[cacheKey] = response
		cacheMutex.Unlock()
		return response
	}

	var results []PropertyResponse
	for _, prop := range properties {
		distance := calculateDistance(targetLat, targetLon, prop.Latitude, prop.Longitude)
		if distance <= 50 {
			results = append(results, PropertyResponse{
				Name:      prop.Name,
				Distance:  distance,
				Latitude:  prop.Latitude,
				Longitude: prop.Longitude,
			})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Distance < results[j].Distance
	})

	var response SearchResponse
	if len(results) == 0 {
		response = SearchResponse{
			Properties: []PropertyResponse{},
			Message:    "No properties found within 50km",
		}
	} else {
		response = SearchResponse{
			Properties: results,
			Message:    fmt.Sprintf("Found %d properties within 50km", len(results)),
		}
	}

	cacheMutex.Lock()
	searchCache[cacheKey] = response
	cacheMutex.Unlock()

	log.Printf("Search completed in %v", time.Since(startTime))
	return response
}

func searchHandler(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		http.Error(w, "Query parameter 'q' is required", http.StatusBadRequest)
		return
	}

	response := searchProperties(query)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func main() {
	r := mux.NewRouter()
	r.HandleFunc("/search", searchHandler).Methods("GET")

	srv := &http.Server{
		Handler:      r,
		Addr:         ":8080",
		WriteTimeout: 2 * time.Second,
		ReadTimeout:  1 * time.Second,
	}

	log.Println("Starting server on :8080")
	log.Fatal(srv.ListenAndServe())
}
