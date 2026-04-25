package hdfc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	platformconfig "gloss/internal/platform/config"
)

type Client struct {
	cfg        platformconfig.HDFCConfig
	httpClient *http.Client
	now        func() time.Time
	traceID    func() string
}

func NewClient(cfg platformconfig.HDFCConfig, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{
		cfg:        cfg,
		httpClient: httpClient,
		now:        func() time.Time { return time.Now().UTC() },
		traceID:    newTraceID,
	}
}

func (c *Client) CreateSale(ctx context.Context, req CreateSaleRequest) (TransactionResponse, error) {
	return c.postEncrypted(ctx, "/API/ecr/v2/saletxn", req.TID, BuildCreateSalePayload(req))
}

func (c *Client) GetTransactionStatus(ctx context.Context, req TransactionStatusRequest) (TransactionResponse, error) {
	return c.postEncrypted(ctx, "/API/ecr/v2/txnstatus", req.TID, BuildTransactionStatusPayload(req))
}

func (c *Client) CancelSale(ctx context.Context, req CancelSaleRequest) (TransactionResponse, error) {
	return c.postEncrypted(ctx, "/API/ecr/v2/canceltxn", req.TID, BuildCancelSalePayload(req))
}

func (c *Client) postEncrypted(ctx context.Context, path string, tid string, innerPayload map[string]any) (TransactionResponse, error) {
	innerJSON, err := json.Marshal(innerPayload)
	if err != nil {
		return TransactionResponse{}, fmt.Errorf("failed to encode HDFC request payload: %w", err)
	}

	encryptedPayload, err := EncryptPayload(innerJSON, c.cfg.ClientSecretKeyHex, c.cfg.IV)
	if err != nil {
		return TransactionResponse{}, err
	}

	outerJSON, err := json.Marshal(envelope{
		PayloadData: encryptedPayload,
		TID:         tid,
	})
	if err != nil {
		return TransactionResponse{}, fmt.Errorf("failed to encode HDFC request envelope: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(path), bytes.NewReader(outerJSON))
	if err != nil {
		return TransactionResponse{}, fmt.Errorf("failed to build HDFC request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("bh_client_apikey", c.cfg.ClientAPIKey)
	httpReq.Header.Set("bh_client_traceid", c.traceID())
	httpReq.Header.Set("bh_client_timestamp", c.now().Format("20060102150405"))
	httpReq.Header.Set("authorizationToken", c.cfg.AuthorizationToken)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return TransactionResponse{}, fmt.Errorf("HDFC request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return TransactionResponse{}, fmt.Errorf("failed to read HDFC response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return TransactionResponse{}, fmt.Errorf("HDFC request returned HTTP %d", resp.StatusCode)
	}

	var outer envelope
	if err := json.Unmarshal(body, &outer); err != nil {
		return TransactionResponse{}, fmt.Errorf("failed to decode HDFC response envelope: %w", err)
	}
	if strings.TrimSpace(outer.PayloadData) == "" {
		return TransactionResponse{}, fmt.Errorf("HDFC response missing encrypted payload")
	}

	plainPayload, err := DecryptPayload(outer.PayloadData, c.cfg.ClientSecretKeyHex, c.cfg.IV)
	if err != nil {
		return TransactionResponse{}, err
	}

	var transactionResponse TransactionResponse
	if err := json.Unmarshal(plainPayload, &transactionResponse); err != nil {
		return TransactionResponse{}, fmt.Errorf("failed to decode HDFC response payload: %w", err)
	}
	transactionResponse.RawPayload = plainPayload

	return transactionResponse, nil
}

func (c *Client) endpoint(path string) string {
	return strings.TrimRight(c.cfg.BaseURL, "/") + path
}

func newTraceID() string {
	return fmt.Sprintf("%d", time.Now().UTC().UnixNano())
}
