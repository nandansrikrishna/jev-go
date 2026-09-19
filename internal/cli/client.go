package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type apiClient struct {
	key, base string
	http      *http.Client
}

func safeError(kind string, status int) object {
	message := "Evaluation failed; check credentials, input, and network"
	if status != 0 {
		message = fmt.Sprintf("TypeSafe request failed (HTTP %d)", status)
	}
	return object{"type": kind, "message": message}
}
func (c *apiClient) evaluate(ctx context.Context, state any, qs object, model string) object {
	body, _ := json.Marshal(object{"state": state, "questions": qs, "model": model})
	started := time.Now()
	var failure object
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/v1/systemone", bytes.NewReader(body))
		if err != nil {
			return object{"error": safeError("TypeSafeAPIConnectionError", 0)}
		}
		req.Header.Set("Authorization", "Bearer "+c.key)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "jev-go/"+Version)
		if attempt > 0 {
			req.Header.Set("X-TypeSafe-Retry-Count", strconv.Itoa(attempt))
		}
		res, err := c.http.Do(req)
		retry := true
		delay := time.Duration(float64(500*time.Millisecond) * math.Pow(2, float64(attempt)) * (1 - rand.Float64()*.25))
		if err != nil {
			failure = safeError("TypeSafeAPIConnectionError", 0)
		} else {
			data, readErr := io.ReadAll(io.LimitReader(res.Body, 32<<20))
			res.Body.Close()
			if readErr != nil {
				failure = safeError("TypeSafeAPIConnectionError", 0)
			} else if res.StatusCode >= 200 && res.StatusCode < 300 {
				if readErr == nil {
					if result, ok := response(data); ok {
						return result
					}
				}
				failure = safeError("TypeSafeAPIResponseValidationError", 0)
				retry = false
			} else {
				failure = safeError("TypeSafeAPIError", res.StatusCode)
				retry = res.StatusCode == 408 || res.StatusCode == 429 || (res.StatusCode >= 500 && res.StatusCode < 600)
				if d, ok := retryDelay(res.Header); ok {
					delay = d
				}
			}
		}
		if !retry || attempt == 2 || time.Since(started)+delay >= 30*time.Second {
			break
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return object{"error": failure}
		case <-timer.C:
		}
	}
	return object{"error": failure}
}
func retryDelay(h http.Header) (time.Duration, bool) {
	for _, spec := range []struct {
		key   string
		scale float64
	}{{"retry-after-ms", float64(time.Millisecond)}, {"Retry-After", float64(time.Second)}} {
		if n, e := strconv.ParseFloat(h.Get(spec.key), 64); e == nil && n >= 0 && !math.IsInf(n, 0) && n*spec.scale < float64(math.MaxInt64) {
			return time.Duration(n * spec.scale), true
		}
	}
	if t, e := http.ParseTime(h.Get("Retry-After")); e == nil {
		return max(0, time.Until(t)), true
	}
	return 0, false
}
func number(v any) bool { _, ok := v.(json.Number); return ok }
func response(data []byte) (object, bool) {
	v, e := decode(data)
	if e != nil {
		return nil, false
	}
	raw, ok := v.(object)
	if !ok {
		return nil, false
	}
	model, ok := raw["model"].(string)
	if !ok {
		return nil, false
	}
	usage, ok := raw["usage"].(object)
	if !ok {
		return nil, false
	}
	cleanUsage := object{}
	for _, k := range []string{"input_tokens", "output_tokens"} {
		if usage[k] != nil {
			n, ok := usage[k].(json.Number)
			if !ok {
				return nil, false
			}
			if _, e := n.Int64(); e != nil {
				return nil, false
			}
			cleanUsage[k] = n
		}
	}
	answers := object{}
	if a, exists := raw["answers"]; exists {
		answers, ok = a.(object)
		if !ok {
			return nil, false
		}
	}
	clean := object{}
	for id, v := range answers {
		a, ok := v.(object)
		if !ok {
			return nil, false
		}
		kind, ok := a["type"].(string)
		if !ok {
			return nil, false
		}
		out := object{"type": kind}
		switch kind {
		case "noul":
			if !number(a["noul"]) {
				return nil, false
			}
			out["noul"] = a["noul"]
		case "choice", "score":
			if !number(a["confidence"]) {
				return nil, false
			}
			out["confidence"] = a["confidence"]
			probs, ok := a["probabilities"].(object)
			if !ok {
				return nil, false
			}
			for k, p := range probs {
				if !number(p) {
					return nil, false
				}
				if kind == "score" {
					if _, e := strconv.Atoi(k); e != nil {
						return nil, false
					}
				}
			}
			out["probabilities"] = probs
			if kind == "choice" {
				if _, ok := a["choice"].(string); !ok {
					return nil, false
				}
				out["choice"] = a["choice"]
			} else {
				if !number(a["score"]) {
					return nil, false
				}
				out["score"] = a["score"]
				legend, ok := a["legend"].(object)
				if !ok {
					return nil, false
				}
				for k, v := range legend {
					if _, e := strconv.Atoi(k); e != nil || !content(v) {
						return nil, false
					}
				}
				out["legend"] = legend
			}
		default:
			continue
		}
		clean[id] = out
	}
	return object{"model": model, "usage": cleanUsage, "answers": clean}, true
}

func newAPIClient() (*apiClient, error) {
	key, e := apiKey()
	if e != nil {
		return nil, e
	}
	base := strings.TrimRight(os.Getenv("TYPESAFE_BASE_URL"), "/")
	if base == "" {
		base = "https://api.typesafe.ai"
	}
	u, e := url.Parse(base)
	if e != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errInput
	}
	client := &apiClient{key: key, base: base, http: &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	return client, nil
}
