package network

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"time"
)

// Result represents the metrics collected at the network level.
type Result struct {
	DNSLookupMS    float64 `json:"dns_lookup_ms"`
	TCPConnectMS   float64 `json:"tcp_connect_ms"`
	TLSHandshakeMS float64 `json:"tls_handshake_ms"`
	TTFBMS         float64 `json:"ttfb_ms"`
	TransferMS     float64 `json:"transfer_ms"`
	TotalMS        float64 `json:"total_ms"`
	StatusCode     int     `json:"status_code"`
	ResponseBytes  int64   `json:"response_bytes"`
}

// Collect performs a network-level timing trace for the given URL.
func Collect(ctx context.Context, url string) (*Result, error) {
	var (
		dnsStart, dnsDone           time.Time
		connStart, connDone         time.Time
		tlsStart, tlsDone           time.Time
		gotFirstByte, gotResponse   time.Time
	)

	trace := &httptrace.ClientTrace{
		DNSStart: func(_ httptrace.DNSStartInfo) { dnsStart = time.Now() },
		DNSDone:  func(_ httptrace.DNSDoneInfo) { dnsDone = time.Now() },
		ConnectStart: func(_, _ string) {
			if dnsDone.IsZero() {
				dnsDone = time.Now()
			}
			connStart = time.Now()
		},
		ConnectDone: func(_, _ string, _ error) { connDone = time.Now() },
		TLSHandshakeStart: func() { tlsStart = time.Now() },
		TLSHandshakeDone:  func(_ tls.ConnectionState, _ error) { tlsDone = time.Now() },
		GotFirstResponseByte: func() { gotFirstByte = time.Now() },
	}

	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), "GET", url, nil)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	n, err := io.Copy(io.Discard, resp.Body)
	if err != nil {
		return nil, err
	}
	gotResponse = time.Now()

	res := &Result{
		StatusCode:    resp.StatusCode,
		ResponseBytes: n,
		TotalMS:       float64(gotResponse.Sub(start).Nanoseconds()) / 1e6,
	}

	if !dnsDone.IsZero() && !dnsStart.IsZero() {
		res.DNSLookupMS = float64(dnsDone.Sub(dnsStart).Nanoseconds()) / 1e6
	}
	if !connDone.IsZero() && !connStart.IsZero() {
		res.TCPConnectMS = float64(connDone.Sub(connStart).Nanoseconds()) / 1e6
	}
	if !tlsDone.IsZero() && !tlsStart.IsZero() {
		res.TLSHandshakeMS = float64(tlsDone.Sub(tlsStart).Nanoseconds()) / 1e6
	}
	if !gotFirstByte.IsZero() {
		res.TTFBMS = float64(gotFirstByte.Sub(start).Nanoseconds()) / 1e6
	}
	if !gotFirstByte.IsZero() {
		res.TransferMS = float64(gotResponse.Sub(gotFirstByte).Nanoseconds()) / 1e6
	}

	if resp.StatusCode >= 400 {
		return res, fmt.Errorf("server returned status code %d", resp.StatusCode)
	}

	return res, nil
}
