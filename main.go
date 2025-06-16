package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"io"
	"log"
	"os"

	"prebid-fastly-compute/adapter/smartadserver"

	"github.com/fastly/compute-sdk-go/fsthttp"
	"github.com/prebid/openrtb/v20/openrtb2"
)

//go:embed pbs.yaml
var pbsConfig embed.FS

func main() {
	logger := log.New(os.Stdout, "[PBS-WASM] ", log.LstdFlags|log.Lmicroseconds)
	logger.Printf("=== Starting PBS WASM ===")

	// Read the embedded PBS config
	configBytes, err := pbsConfig.ReadFile("pbs.yaml")
	if err != nil {
		logger.Printf("ERROR: Failed to read PBS config: %v", err)
		return
	}
	logger.Printf("SUCCESS: Loaded PBS config (%d bytes)", len(configBytes))

	// Log the actual config content for debugging
	logger.Printf("Config content: %s", string(configBytes))

	// Initialize SmartAdServer adapter
	builder := &smartadserver.Builder{}
	logger.Printf("Created SmartAdServer builder")

	bidder, err := builder.BuildBidder(configBytes)
	if err != nil {
		logger.Printf("ERROR: Failed to build SmartAdServer bidder: %v", err)
		return
	}
	logger.Printf("SUCCESS: Built SmartAdServer bidder")

	// Handle requests using Fastly's fsthttp
	fsthttp.ServeFunc(func(ctx context.Context, w fsthttp.ResponseWriter, r *fsthttp.Request) {
		if r.URL.Path != "/openrtb2/auction" {
			w.WriteHeader(fsthttp.StatusNotFound)
			return
		}

		logger.Printf("Received request to /openrtb2/auction")

		// Read the request body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			logger.Printf("ERROR: Failed to read request body: %v", err)
			w.WriteHeader(fsthttp.StatusBadRequest)
			return
		}

		// Parse the bid request
		var bidRequest openrtb2.BidRequest
		if err := json.Unmarshal(body, &bidRequest); err != nil {
			logger.Printf("ERROR: Failed to parse bid request: %v", err)
			w.WriteHeader(fsthttp.StatusBadRequest)
			return
		}

		// Log the incoming bid request
		bidRequestJSON, _ := json.MarshalIndent(&bidRequest, "", "  ")
		logger.Printf("Incoming Bid Request: %s", string(bidRequestJSON))

		// Make bid requests
		logger.Printf("Making bid requests...")
		httpRequests, errs := bidder.MakeRequests(&bidRequest)
		if len(errs) > 0 {
			logger.Printf("ERROR: Failed to make requests:")
			for i, err := range errs {
				logger.Printf("  Error %d: %v", i+1, err)
			}
			w.WriteHeader(fsthttp.StatusInternalServerError)
			return
		}
		logger.Printf("SUCCESS: Created %d HTTP requests", len(httpRequests))

		// Process each request
		for i, req := range httpRequests {
			logger.Printf("Request %d:", i+1)
			logger.Printf("  Method: %s", req.Method)
			logger.Printf("  URI: %s", req.Uri)
			logger.Printf("  Headers:")
			for k, v := range req.Headers {
				logger.Printf("    %s: %s", k, v)
			}
			logger.Printf("  Body: %s", string(req.Body))

			// Create backend request
			bereq, err := fsthttp.NewRequest(req.Method, req.Uri, bytes.NewReader(req.Body))
			if err != nil {
				logger.Printf("ERROR: Failed to create backend request: %v", err)
				continue
			}

			// Set headers
			logger.Printf("Setting request headers for SmartAdServer:")
			for k, v := range req.Headers {
				logger.Printf("  %s: %s", k, v)
				bereq.Header.Set(k, v)
			}

			// Log full request details before sending
			logger.Printf("Sending request to SmartAdServer:")
			logger.Printf("  Full URL: %s", bereq.URL.String())
			logger.Printf("  Method: %s", bereq.Method)
			logger.Printf("  Headers:")
			for k, v := range bereq.Header {
				logger.Printf("    %s: %s", k, v)
			}
			logger.Printf("  Body: %s", string(req.Body))

			// Send request to backend using the static backend name
			logger.Printf("Attempting to send request to backend 'smartadserver_backend'...")
			beresp, err := bereq.Send(ctx, "smartadserver_backend")
			if err != nil {
				logger.Printf("ERROR: Failed to send request: %v", err)
				continue
			}
			defer beresp.Body.Close()

			// Read and log response details
			respBody, err := io.ReadAll(beresp.Body)
			if err != nil {
				logger.Printf("ERROR: Failed to read response: %v", err)
				continue
			}

			logger.Printf("Response from SmartAdServer:")
			logger.Printf("  Status: %d", beresp.StatusCode)
			logger.Printf("  Response Headers:")
			for k, v := range beresp.Header {
				logger.Printf("    %s: %s", k, v)
			}
			logger.Printf("  Body: %s", string(respBody))

			if beresp.StatusCode == 404 {
				logger.Printf("WARNING: Received 404 from SmartAdServer - common causes:")
				logger.Printf("  1. Incorrect endpoint URL")
				logger.Printf("  2. Missing required headers")
				logger.Printf("  3. Invalid request format")
				logger.Printf("  4. Invalid site/page/format IDs")
			}

			// Send response back to the client
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(beresp.StatusCode)
			w.Write(respBody)
			return // Return after first successful response
		}

		// If we get here, no requests were successful
		w.WriteHeader(fsthttp.StatusInternalServerError)
	})
}
