package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"

	log "github.com/sirupsen/logrus"
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

var (
	STOCK_ENDPOINT="https://www.alphavantage.co/query?function=TIME_SERIES_INTRADAY&outputsize=full&symbol=%s&interval=5min&apikey=%s"
	STOCK_API_KEY=os.Getenv("ALPHA_KEY")
)

func main() {
	log.Infof("got stock %+v", getTickerData("GME"))
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