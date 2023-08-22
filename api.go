package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/showwin/speedtest-go/speedtest"
)

func main() {
	fmt.Print("TESTE")
	http.HandleFunc("/api/test", HandleTest)
	log.Fatal(http.ListenAndServe(":9090", nil))
}

func HandleTest(w http.ResponseWriter, r *http.Request) {
	fmt.Print("HANDLE TEST")
	serverList, err := speedtest.FetchServers()
	if err != nil {
		http.Error(w, "Failed to fetch server list", http.StatusInternalServerError)
		return
	}

	targets, err := serverList.FindServer([]int{})
	if err != nil {
		http.Error(w, "Failed to find server", http.StatusInternalServerError)
		return
	}

	results := []map[string]interface{}{}

	for _, s := range targets {
		err := s.PingTest(nil)
		if err != nil {
			http.Error(w, "Failed to perform ping test", http.StatusInternalServerError)
			return
		}
		err = s.DownloadTest()
		if err != nil {
			http.Error(w, "Failed to perform download test", http.StatusInternalServerError)
			return
		}
		err = s.UploadTest()
		if err != nil {
			http.Error(w, "Failed to perform upload test", http.StatusInternalServerError)
			return
		}

		result := map[string]interface{}{
			"Latency":  s.Latency,
			"Download": s.DLSpeed,
			"Upload":   s.ULSpeed,
		}
		results = append(results, result)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}