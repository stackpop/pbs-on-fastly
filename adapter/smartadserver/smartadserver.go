package smartadserver

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"gopkg.in/yaml.v2"

	"prebid-fastly-compute/adapter"

	"github.com/prebid/openrtb/v20/openrtb2"
)

// PBSConfig represents the PBS YAML configuration structure
type PBSConfig struct {
	Adapters struct {
		SmartAdServer struct {
			Enabled       bool   `yaml:"enabled"`
			Endpoint      string `yaml:"endpoint"`
			PlatformID    int    `yaml:"platform-id"`
			DefaultConfig struct {
				SiteID     int `yaml:"site-id"`
				PageID     int `yaml:"page-id"`
				FormatID   int `yaml:"format-id"`
				PlatformID int `yaml:"platform-id"`
			} `yaml:"default-config"`
		} `yaml:"smartadserver"`
	} `yaml:"adapters"`
}

// Builder builds a new SmartAdServer bidder instance
type Builder struct{}

// BuildBidder creates a new bidding endpoint
func (b *Builder) BuildBidder(config []byte) (adapter.Bidder, error) {
	var pbsConfig PBSConfig
	if err := yaml.Unmarshal(config, &pbsConfig); err != nil {
		return nil, fmt.Errorf("error parsing PBS config: %v", err)
	}

	if !pbsConfig.Adapters.SmartAdServer.Enabled {
		return nil, fmt.Errorf("SmartAdServer adapter is not enabled in config")
	}

	adapter := &SmartAdServerAdapter{
		endpoint:   pbsConfig.Adapters.SmartAdServer.Endpoint,
		platformID: pbsConfig.Adapters.SmartAdServer.PlatformID,
	}
	adapter.defaultConfig.SiteID = pbsConfig.Adapters.SmartAdServer.DefaultConfig.SiteID
	adapter.defaultConfig.PageID = pbsConfig.Adapters.SmartAdServer.DefaultConfig.PageID
	adapter.defaultConfig.FormatID = pbsConfig.Adapters.SmartAdServer.DefaultConfig.FormatID
	adapter.defaultConfig.PlatformID = pbsConfig.Adapters.SmartAdServer.DefaultConfig.PlatformID

	return adapter, nil
}

// SmartAdServerAdapter implements Bidder interface for SmartAdServer
type SmartAdServerAdapter struct {
	endpoint      string
	platformID    int
	defaultConfig struct {
		SiteID     int `yaml:"site-id"`
		PageID     int `yaml:"page-id"`
		FormatID   int `yaml:"format-id"`
		PlatformID int `yaml:"platform-id"`
	}
}

// MakeRequests creates http requests to fetch bids
func (a *SmartAdServerAdapter) MakeRequests(request *openrtb2.BidRequest) ([]*adapter.HttpRequest, []error) {
	log.Printf("[SmartAdServer] Starting bid request to endpoint: %s", a.endpoint)
	log.Printf("[SmartAdServer] Configured values:")
	log.Printf("  SiteID: %d", a.defaultConfig.SiteID)
	log.Printf("  PageID: %d", a.defaultConfig.PageID)
	log.Printf("  FormatID: %d", a.defaultConfig.FormatID)
	log.Printf("  NetworkID: %d", a.platformID)

	// Enrich the request with SmartAdServer specific parameters
	if len(request.Imp) == 0 {
		return nil, []error{fmt.Errorf("no impressions in bid request")}
	}

	// Create a copy of the first impression to modify
	imp := request.Imp[0]

	// Add SmartAdServer specific extension
	impExt := map[string]interface{}{
		"prebid": map[string]interface{}{
			"bidder": map[string]interface{}{
				"smartadserver": map[string]interface{}{
					"siteId":    a.defaultConfig.SiteID,
					"networkId": a.platformID,
					"pageId":    a.defaultConfig.PageID,
					"formatId":  a.defaultConfig.FormatID,
					"target":    "testing=prebid",
					"domain":    request.Site.Domain, // Add domain if available
				}},
		},
	}

	// Marshal the extension
	impExtJSON, err := json.Marshal(impExt)
	if err != nil {
		return nil, []error{fmt.Errorf("error marshaling imp.ext: %v", err)}
	}

	// Set the extension on the impression
	imp.Ext = impExtJSON

	// Ensure we have banner format information
	if imp.Banner == nil {
		imp.Banner = &openrtb2.Banner{
			Format: []openrtb2.Format{
				{W: 728, H: 90}, // Default banner size
			},
		}
	}

	// Set bidfloor information
	imp.BidFloor = 0.01
	imp.BidFloorCur = "USD"

	// Create enriched bid request
	enrichedRequest := *request // Make a copy
	enrichedRequest.Imp = []openrtb2.Imp{imp}
	enrichedRequest.Test = 1
	enrichedRequest.TMax = 1000
	enrichedRequest.AT = 1

	// Add site information if not present
	if enrichedRequest.Site == nil {
		enrichedRequest.Site = &openrtb2.Site{}
	}
	if enrichedRequest.Site.Page == "" && request.Site != nil && request.Site.Domain != "" {
		enrichedRequest.Site.Page = fmt.Sprintf("https://%s", request.Site.Domain)
	}

	// Marshal the enriched request
	reqBody, err := json.Marshal(&enrichedRequest)
	if err != nil {
		return nil, []error{err}
	}

	log.Printf("[SmartAdServer] Full request details:")
	log.Printf("  URL: %s", a.endpoint)
	log.Printf("  Method: POST")
	log.Printf("  Headers:")
	headers := map[string]string{
		"Content-Type":      "application/json;charset=utf-8",
		"Accept":            "application/json",
		"X-Openrtb-Version": "2.5",
	}
	for k, v := range headers {
		log.Printf("    %s: %s", k, v)
	}
	log.Printf("  Body: %s", string(reqBody))

	return []*adapter.HttpRequest{
		{
			Method:  "POST",
			Uri:     a.endpoint,
			Body:    reqBody,
			Headers: headers,
		},
	}, nil
}

// MakeBids unpacks the server's response into Bids
func (a *SmartAdServerAdapter) MakeBids(request *openrtb2.BidRequest, response *adapter.HttpResponse) (*adapter.BidderResponse, []error) {
	log.Printf("[SmartAdServer] Response status code: %d", response.StatusCode)
	log.Printf("[SmartAdServer] Response headers: %v", response.Headers)
	log.Printf("[SmartAdServer] Response body: %s", string(response.Body))

	if response.StatusCode == http.StatusNoContent {
		log.Printf("[SmartAdServer] No content in response")
		return nil, nil
	}

	if response.StatusCode == http.StatusBadRequest {
		log.Printf("[SmartAdServer] Bad request error")
		return nil, []error{fmt.Errorf("Bad request: %s", string(response.Body))}
	}

	if response.StatusCode == http.StatusNotFound {
		log.Printf("[SmartAdServer] 404 Not Found error. Common causes:")
		log.Printf("1. Incorrect endpoint URL path")
		log.Printf("2. Invalid or missing caller ID")
		log.Printf("3. Invalid site ID, page ID, or format ID")
		log.Printf("4. Request not properly formatted")
		return nil, []error{fmt.Errorf("404 Not Found: %s", string(response.Body))}
	}

	if response.StatusCode != http.StatusOK {
		log.Printf("[SmartAdServer] Unexpected status code: %d", response.StatusCode)
		return nil, []error{fmt.Errorf("Unexpected status code: %d. Body: %s", response.StatusCode, string(response.Body))}
	}

	var bidResp openrtb2.BidResponse
	if err := json.Unmarshal(response.Body, &bidResp); err != nil {
		return nil, []error{err}
	}

	bidResponse := adapter.BidderResponse{
		Bids: make([]*adapter.TypedBid, 0),
	}

	for _, seatBid := range bidResp.SeatBid {
		for i := range seatBid.Bid {
			bidResponse.Bids = append(bidResponse.Bids, &adapter.TypedBid{
				Bid:     &seatBid.Bid[i],
				BidType: "banner", // SmartAdServer typically returns banner ads
			})
		}
	}

	return &bidResponse, nil
}
