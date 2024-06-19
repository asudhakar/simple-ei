package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/asudhakar/simple-ei/config"
)

type EITableData struct {
	Province             string  `json:"province"`
	EconomicRegionCode   int     `json:"economic_region_code"`
	EconomicRegionName   string  `json:"economic_region_name"`
	UnemploymentRate     float64 `json:"unemployment_rate"`
	InsuredHoursRequired int     `json:"insured_hours_required"`
	MinWeeksPayable      int     `json:"min_weeks_payable"`
	MaxWeeksPayable      int     `json:"max_weeks_payable"`
	BestWeeksRequired    int     `json:"best_weeks_required"`
}

type RequestPayload struct {
	PostalCode string `json:"postal_code"`
}

type ResponsePayload struct {
	PostalCode string        `json:"postal_code"`
	Data       []EITableData `json:"data"`
}

func scrapeTableURLs(url, tableID string) ([]string, error) {
	urls := make([]string, 0)
	res, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Error: Status code %d", res.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		return nil, err
	}

	table := doc.Find(fmt.Sprintf("#%s", tableID))
	if table.Length() == 0 {
		return nil, fmt.Errorf("Error: No table found with ID %s", tableID)
	}

	table.Find("tr").Each(func(i int, row *goquery.Selection) {
		row.Find("td").Each(func(j int, cell *goquery.Selection) {
			cell.Find("a").Each(func(k int, anchor *goquery.Selection) {
				href, exists := anchor.Attr("href")
				if exists {
					urls = append(urls, href)
				}
			})
		})
	})
	return urls, nil
}

func scrapeTable(url, tableID string) ([]EITableData, error) {
	var tableDatas []EITableData
	res, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Error: Status code %d", res.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		return nil, err
	}

	table := doc.Find(fmt.Sprintf("#%s", tableID))
	if table.Length() == 0 {
		return nil, fmt.Errorf("Error: No table found with ID %s", tableID)
	}

	table.Find("tr").Each(func(i int, row *goquery.Selection) {
		var tableData EITableData
		row.Find("td").Each(func(j int, cell *goquery.Selection) {
			switch j {
			case 0:
				tableData.Province = cell.Text()
			case 1:
				fmt.Sscan(cell.Text(), &tableData.EconomicRegionCode)
			case 2:
				tableData.EconomicRegionName = cell.Text()
			case 3:
				fmt.Sscan(cell.Text(), &tableData.UnemploymentRate)
			case 4:
				fmt.Sscan(cell.Text(), &tableData.InsuredHoursRequired)
			case 5:
				fmt.Sscan(cell.Text(), &tableData.MinWeeksPayable)
			case 6:
				fmt.Sscan(cell.Text(), &tableData.MaxWeeksPayable)
			case 7:
				fmt.Sscan(cell.Text(), &tableData.BestWeeksRequired)
			}
		})

		// Check if any of the fields have been populated
		if tableData.Province != "" || tableData.EconomicRegionCode != 0 || tableData.EconomicRegionName != "" || tableData.UnemploymentRate != 0 || tableData.InsuredHoursRequired != 0 || tableData.MinWeeksPayable != 0 || tableData.MaxWeeksPayable != 0 || tableData.BestWeeksRequired != 0 {
			tableDatas = append(tableDatas, tableData)
		}
	})
	return tableDatas, nil
}

func processFunc(c config.Configuration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var reqPayload RequestPayload
		if err := json.NewDecoder(r.Body).Decode(&reqPayload); err != nil {
			writeJSONResponse(w, http.StatusBadRequest, "Error parsing JSON request body", nil)
			return
		}

		urls, err := scrapeTableURLs(c.BaseURL+c.PostalCodeEndpoint+reqPayload.PostalCode, c.PostalCodePageTableID)
		if err != nil {
			writeJSONResponse(w, http.StatusInternalServerError, err.Error(), nil)
			return
		}

		var tableDatas []EITableData
		for _, subUrl := range urls {
			data, err := scrapeTable(c.BaseURL+subUrl, c.EIPageTableID)
			if err != nil {
				writeJSONResponse(w, http.StatusInternalServerError, err.Error(), nil)
				return
			}
			tableDatas = append(tableDatas, data...)
		}

		respPayload := ResponsePayload{
			PostalCode: reqPayload.PostalCode,
			Data:       tableDatas,
		}

		writeJSONResponse(w, http.StatusOK, "success", respPayload)
	}
}

func writeJSONResponse(w http.ResponseWriter, statusCode int, message string, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": message,
		"data":    data,
	})
}

func enableCors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	router := http.NewServeMux()
	router.Handle("/health", enableCors(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })))
	router.Handle("/process", enableCors(http.HandlerFunc(processFunc(cfg))))

	server := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: router,
	}

	go func() {
		fmt.Printf("Server listening on localhost:%s\n", cfg.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	fmt.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		fmt.Printf("Server forced to shutdown: %v\n", err)
	} else {
		fmt.Println("Server gracefully stopped")
	}
}
