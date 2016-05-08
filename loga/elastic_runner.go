package loga

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/malnick/logasaurus/config"

	"github.com/mgutz/ansi"
)

type ESRequest struct {
	Size int `json:"size"`
	Sort struct {
		Timestamp string `json:"@timestamp"`
	} `json:"sort"`
	Query struct {
		Filtered struct {
			Query struct {
				QueryString struct {
					AnalyzeWildcard string `json:"analyze_wildcard"`
					Query           string `json:"query"`
				} `json:"query_string"`
			} `json:"query"`
			Filter struct {
				Bool struct {
					Must    []ESMust    `json:"must"`
					MustNot []ESMustNot `json:"must_not"`
				} `json:"bool"`
			} `json:"filter"`
		} `json:"filtered"`
	} `json:"query"`
}

type ESMust struct {
	Range struct {
		Timestamp struct {
			Gte interface{} `json:"gte"`
			Lte interface{} `json:"lte"`
		} `json:"@timestamp"`
	} `json:"range"`
}

type ESMustNot struct{}

func (esRequest *ESRequest) makeRequest(c *config.Config) (ESResponse, error) {
	var esResponse ESResponse

	jsonpost, err := json.MarshalIndent(&esRequest, "", "\t")
	if err != nil {
		return esResponse, err
	}
	log.Debugf("Elastic Search Request:\n %s", string(jsonpost))

	// Craft the request URI
	requestMethod := "http"
	if c.EnableSSL {
		requestMethod := "https"
	}
	queryURL := strings.Join([]string{requestMethod, "://", c.ElasticsearchURL, ":", c.ElasticsearchPort, "/_search?pretty"}, "")
	log.Debug("Query URI: ", queryURL)

	// Make request
	req, err := http.NewRequest("POST", queryURL, bytes.NewBuffer(jsonpost))
	if err != nil {
		return esResponse, err
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return esResponse, err
	}
	defer resp.Body.Close()

	jsonRespBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return esResponse, err
	}
	log.Debugf("Elastic Search Response:\n%s", string(jsonRespBody))

	err = json.Unmarshal(jsonRespBody, &esResponse)
	if err != nil {
		return esResponse, err
	}
	CheckElasticResponse(&esResponse)

	return esResponse, nil
}

type Hit struct {
	Source struct {
		Host    string `json:"host"`
		Message string `json:"message"`
	} `json:"_source"`
}

type ESResponse struct {
	Hits struct {
		Hits []Hit `json:"hits"`
	} `json:"hits"`
	Status int `json:"status"`
}

func (esResponse *ESResponse) printResponse(c config.Config, service string) {
	// Print
	for _, hit := range esResponse.Hits.Hits {
		if c.SearchHost {
			message := hit.Source.Message
			host := ansi.Color(hit.Source.Host, "cyan:black")
			withHost := strings.Join([]string{host, " ", message}, "")
			if c.Highlight {
				highlightQuery(withHost, service)
			} else {
				fmt.Println(withHost)
			}
		} else {
			message := hit.Source.Message
			if c.Highlight {
				highlightQuery(message, service)
			} else {
				fmt.Println(message)
			}
		}
	}
}

func elasticRunner(service string, c config.Config) {
	var (
		esRequest = ESRequest{}
		must      = ESMust{}
		lte       = time.Now().Add(time.Duration(-c.StartTime) * time.Minute)
	)
	for syncCount := 0; syncCount >= 0; syncCount++ {
		// Set time: last 10min or last sync_interval
		if syncCount > 0 {
			must.Range.Timestamp.Gte = lte.Add(time.Duration(-c.SyncInterval) * time.Second)
		} else {
			must.Range.Timestamp.Gte = lte.Add(time.Duration(-c.SyncDepth) * time.Minute)
		}

		must.Range.Timestamp.Lte = lte

		esRequest.Size = c.Count
		esRequest.Sort.Timestamp = "asc"
		esRequest.Query.Filtered.Query.QueryString.AnalyzeWildcard = "true"
		esRequest.Query.Filtered.Query.QueryString.Query = string(service)
		esRequest.Query.Filtered.Filter.Bool.Must = []ESMust{must}

		esResponse, err := esRequest.makeRequest(&c)
		BasicCheckOrExit(err)

		esResponse.printResponse(c, service)
		time.Sleep(time.Second * time.Duration(c.SyncInterval))
	}
}
