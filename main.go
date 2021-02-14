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
	"github.com/gocolly/colly"
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
		/*
		2021-02-12 19:50:00": {
		            "1. open": "120.9000",
		            "2. high": "120.9000",
		            "3. low": "120.9000",
		            "4. close": "120.9000",
		            "5. volume": "300"
		        },
		*/
	} `json:"Time Series (5min)"`
}

type StockDetail struct {
	Ticker string `json:"Symbol"`
	Name string `json:"Name"`
	Description string `json:"Description"`
}

type StockAPIRes struct {
	StockRes
	StockDetail
}

type TimeSeries struct {
	ClosePrice float64 `json:"close_price"`
	Volume float64 `json:"volume"`
}

type Stock struct {
	Details struct {
		Description string `json:"description"`
		EnvironmentalSummary string `json:"environmental_summary"`
		EnvironmentalArticles map[string]*string `json:"environmental_articles"`
		RawSentiment string `json:"raw_sentiment"`
		EnvironmentalScore float64 `json:"environmental_score"`
		EnvironmentalSentiment []string `json:"environmental_sentiment"`
	} `json:"details"`
	TimeData map[string]*TimeSeries `json:"time_data"`
}

type ReturnData struct {
	Stocks map[string]*Stock `json:"stocks"`
}

var (
	STOCK_TIME_API_ENDPOINT   ="https://www.alphavantage.co/query?function=TIME_SERIES_INTRADAY&outputsize=full&symbol=%s&interval=5min&apikey=%s"
	STOCK_DETAIL_API_ENDPOINT ="https://www.alphavantage.co/query?function=OVERVIEW&symbol=%s&apikey=%s"
	STOCK_API_KEY             =os.Getenv("ALPHA_KEY")
	OPEN_AI_API_KEY           =os.Getenv("OPEN_AI_KEY")
	STOCK_LIST_FILE           ="stocks.csv"
	SEARCH_URL="https://duckduckgo.com/?q=%s+environmental+impact" // replace any space with '+'
)

func main() {
	tickers := make(chan string, 52)
	stocks := make(chan *StockAPIRes, 52)
	go getStockTickers(tickers)
	go func(tickers chan string, stocks chan *StockAPIRes) {
		for {
			ticker, ok := <-tickers
			if !ok {
				return
			}
			go func(tickers chan string, stocks chan *StockAPIRes) {
				stockRes := getTickerData(fmt.Sprintf("%s", ticker))
				stockDetails := getTickerDetails(fmt.Sprintf("%s", ticker))
				stockTotal := &StockAPIRes{
					*stockRes,
					*stockDetails,
				}
				stockTotal.Ticker = ticker
				stocks <- stockTotal
				log.Infof("got stock %s", ticker)
				return
			}(tickers, stocks)
		}
	}(tickers, stocks)
	stockReturn := ReturnData{}
	stockReturn.Stocks = make(map[string]*Stock)
	openAIClient := gogpt.NewClient(OPEN_AI_API_KEY)
	ctx := context.Background()
	for stock := range stocks {
		log.Printf("STOCK IS %+v, STOCK DETAIL IS %+v", stock, stock.StockDetail)
		stockReturn.Stocks[stock.StockDetail.Ticker] = &Stock{}
		stockReturn.Stocks[stock.StockDetail.Ticker].Details.Description = stock.Description
		req := gogpt.CompletionRequest{
			MaxTokens: 1,
			Prompt: fmt.Sprintf(`Description: "%s"\nSentiment (positive, neutral, negative):`, stock.Description),
		}
		sentimentRes, err := openAIClient.CreateCompletion(ctx, "davinci-instruct-beta", req)
		if err != nil {
			log.Errorf("failed to get sentiment from OpenAI for ticker %s", stock.StockDetail.Ticker)
		}
		rawSentiment := sentimentRes.Choices[0].Text
		stockReturn.Stocks[stock.StockDetail.Ticker].Details.RawSentiment = rawSentiment

		c := colly.NewCollector(
			colly.MaxDepth(1),
			colly.Async(true),
			)
		detailCollector := c.Clone()
		// Find and visit all links
		c.OnHTML("a[href]", func(e *colly.HTMLElement) {
			if e.Attr("class") == "result__url" {
				placeholder := ""
				stockReturn.Stocks[stock.StockDetail.Ticker].Details.EnvironmentalArticles[e.Attr("href")] = &placeholder
				e.Request.Visit(e.Attr("href"))
			}
		})

		detailCollector.OnHTML("p", func(e *colly.HTMLElement) {
			childText := *stockReturn.Stocks[stock.StockDetail.Ticker].Details.EnvironmentalArticles[e.Request.URL.String()] + "\n" + e.ChildText("p")
			stockReturn.Stocks[stock.StockDetail.Ticker].Details.EnvironmentalArticles[e.Request.URL.String()] = &childText
		})

		detailCollector.OnHTML("span", func(e *colly.HTMLElement) {
			childText := *stockReturn.Stocks[stock.StockDetail.Ticker].Details.EnvironmentalArticles[e.Request.URL.String()] + "\n" + e.ChildText("span")
			stockReturn.Stocks[stock.StockDetail.Ticker].Details.EnvironmentalArticles[e.Request.URL.String()] = &childText
		})

		c.OnRequest(func(r *colly.Request) {
			log.Infof("Visiting %s", r.URL.String())
		})
		log.Infof("STOCK TICKER IS %v", stock.StockDetail.Ticker)
		c.Visit(fmt.Sprintf(SEARCH_URL, stock.StockDetail.Ticker))
		c.Wait()

		for k, v := range stockReturn.Stocks[stock.StockDetail.Ticker].Details.EnvironmentalArticles {
			req = gogpt.CompletionRequest{
				MaxTokens: 1,
				Prompt: fmt.Sprintf(`%s\n\ntl;dr:`, v),
			}
			summaryRes, err := openAIClient.CreateCompletion(ctx, "davinci-instruct-beta", req)
			if err != nil {
				log.Errorf("failed to get sentiment from OpenAI for ticker %s, url %s", stock.StockDetail.Ticker, k)
			}
			summary := summaryRes.Choices[0].Text
			req = gogpt.CompletionRequest{
				MaxTokens: 1,
				Prompt: fmt.Sprintf(`Description: "%s"\nSentiment (positive, neutral, negative):`, summary),
			}
			sentimentURLRes, err := openAIClient.CreateCompletion(ctx, "davinci-instruct-beta", req)
			if err != nil {
				log.Errorf("failed to get sentiment from OpenAI for ticker %s, url %s", stock.StockDetail.Ticker, k)
			}
			rawURLSentiment := sentimentURLRes.Choices[0].Text
			stockReturn.Stocks[stock.StockDetail.Ticker].Details.EnvironmentalSentiment = append(stockReturn.Stocks[stock.StockDetail.Ticker].Details.EnvironmentalSentiment, rawURLSentiment)
			log.Infof("sentiment of article %s is %s", k, rawURLSentiment)
			log.Infof("stock %s article %s sentiment %s", stock.StockDetail.Ticker, k, rawURLSentiment)
		}
		/*
		for past 15 articles (search by stock.name+environmental impact):
			summarize
			sentiment on summaries
		equations
		transform time series to return time
		post to firebase
		*/
	}
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
	resp, err := http.Get(fmt.Sprintf(STOCK_TIME_API_ENDPOINT, ticker, STOCK_API_KEY))
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

func getTickerDetails(ticker string) *StockDetail {
	log.Infof("getting stock details for %s ticker...", ticker)
	resp, err := http.Get(fmt.Sprintf(STOCK_DETAIL_API_ENDPOINT, ticker, STOCK_API_KEY))
	if err != nil {
		log.Errorf("failed to get stock details for %s ticker: %s", ticker, err)
	}

	defer resp.Body.Close()
	bodyBytes, _ := ioutil.ReadAll(resp.Body)

	// Convert response body to StockRes struct
	var stock StockDetail
	json.Unmarshal(bodyBytes, &stock)
	log.Infof("unmarshalled stock details for %s ticker...", ticker)

	return &stock
}