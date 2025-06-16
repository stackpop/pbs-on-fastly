package adapter

import (
	"github.com/prebid/openrtb/v20/openrtb2"
)

// HttpRequest represents an outgoing HTTP request to a bidder
type HttpRequest struct {
	Method  string
	Uri     string
	Body    []byte
	Headers map[string]string
}

// HttpResponse represents a response from a bidder
type HttpResponse struct {
	StatusCode int
	Body       []byte
	Headers    map[string]string
}

// TypedBid includes the openrtb2.Bid and the specific type of bid (banner, video, etc)
type TypedBid struct {
	Bid     *openrtb2.Bid
	BidType string
}

// BidderResponse wraps the server's response with the list of bids
type BidderResponse struct {
	Bids []*TypedBid
}

// Bidder interface for bidding
type Bidder interface {
	// MakeRequests makes the HTTP requests to the bidder
	MakeRequests(request *openrtb2.BidRequest) ([]*HttpRequest, []error)

	// MakeBids unpacks the server's response into Bids
	MakeBids(request *openrtb2.BidRequest, response *HttpResponse) (*BidderResponse, []error)
}

// Builder builds a new instance of the bidder
type Builder interface {
	BuildBidder(params interface{}) (Bidder, error)
}
