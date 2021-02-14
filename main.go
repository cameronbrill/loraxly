package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"

	log "github.com/sirupsen/logrus"
	gogpt "github.com/sashabaranov/go-gpt3"
)

type StockRes struct {
	MetaData struct {
		OneInformation     string `json:"1. Information"`
		TwoSymbol          string `json:"2. Symbol"`
		ThreeLastRefreshed string `json:"3. Last Refreshed"`
		FourInterval       string `json:"4. Interval"`
		FiveOutputSize     string `json:"5. Output Size"`
		SixTimeZone        string `json:"6. Time Zone"`
	} `json:"Meta Data"`
	TimeSeries struct {
		TimeSeriesData map[string]interface{} `json:"-"`
	} `json:"Time Series (5min)"`
}

type returnData struct {
	Stocks map[string]struct{
		Details struct {
			Description string `json:"description"`
			EnvironmentalSummary string `json:"environmental_summary"`
		} `json:"details"`
		TimeData map[string]struct{
			ClosePrice float64 `json:"close_price"`
			Volume float64 `json:"volume"`
			EnvironmentalScore float64 `json:"environmental_score"`
			RawSentiment float64 `json:"raw_sentiment"`
		} `json:"time_data"`
	} `json:"stocks"`
}

var (
	STOCK_ENDPOINT="https://www.alphavantage.co/query?function=TIME_SERIES_INTRADAY&outputsize=full&symbol=%s&interval=5min&apikey=%s"
	STOCK_API_KEY=os.Getenv("ALPHA_KEY")
	OPEN_AI_API_KEY=os.Getenv("OPEN_AI_KEY")
	STOCK_LIST_FILE="stocks.csv"
)

func main() {
	tickers := make(chan string, 52)
	stocks := make(chan *StockRes, 52)
	go getStockTickers(tickers)
	go func(tickers chan string, stocks chan *StockRes) {
		for {
			ticker, ok := <-tickers
			log.Infof("TICKER, OK: %s, %v", ticker, ok)
			if !ok {
				return
			}
			go func(tickers chan string, stocks chan *StockRes) {
				stocks <- getTickerData(fmt.Sprintf("%s", ticker))
				log.Infof("got stock %s", ticker)
				return
			}(tickers, stocks)
		}
	}(tickers, stocks)
	openAIClient := gogpt.NewClient(OPEN_AI_API_KEY)
	select {}
}

func getStockTickers(list chan string) {
	// Open the file
	stockListPath, err := os.Open(STOCK_LIST_FILE)
	if err != nil {
		log.Fatalln("Couldn't open the csv file", err)
	}

	// Parse the file
	r := csv.NewReader(stockListPath)

	// Iterate through the records
	for {
		// Read each record from csv
		record, err := r.Read()
		if err == io.EOF {
			close(list)
			break
		}
		if err != nil {
			log.Errorf("failed to get ticker from stocks.csv: %s", err)
		}
		ticker := record[0]
		list <- ticker
	}
}

func getTickerData(ticker string) *StockRes {
	log.Infof("getting stock data for %s ticker...", ticker)
	resp, err := http.Get(fmt.Sprintf(STOCK_ENDPOINT, ticker, STOCK_API_KEY))
	if err != nil {
		log.Errorf("failed to get stock data for %s ticker: %s", ticker, err)
	}

	defer resp.Body.Close()
	bodyBytes, _ := ioutil.ReadAll(resp.Body)

	respString := string(bodyBytes)

	// Convert response body to StockRes struct
	var stock StockRes
	json.Unmarshal(bodyBytes, &stock)
	//log.Infof("unmarshalled stock info for %s ticker...", ticker)

	json.Unmarshal([]byte(respString), &stock.TimeSeries.TimeSeriesData)
	log.Infof("unmarshalled stock time series for %s ticker...", ticker)

	return &stock
}