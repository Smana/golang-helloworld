package loadgen

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Op is one kind of request.
type Op string

const (
	OpList      Op = "list"
	OpView      Op = "view"
	OpThumbnail Op = "thumbnail"
	OpUpload    Op = "upload"
	OpDelete    Op = "delete"
	OpSettings  Op = "settings"
)

// imagesPath is the images collection endpoint; every per-image path is built from it.
const imagesPath = "/api/images"

var browseTags = []string{"loadgen", "nature", "travel"}

type client struct {
	base     string
	scenario string
	http     *http.Client
	rnd      *lockedRand
	tracer   trace.Tracer

	mu   sync.Mutex
	ids  []int // IDs seen in listings
	mine []int // IDs this run uploaded: the only delete targets
}

func newClient(base, scenario string, rnd *lockedRand) *client {
	return &client{base: base, scenario: scenario, rnd: rnd, tracer: otel.Tracer("image-gallery/loadgen"),
		http: &http.Client{Timeout: 30 * time.Second, Transport: otelhttp.NewTransport(http.DefaultTransport)}}
}

// do runs op under a root span "loadgen <op>"; otelhttp adds the CLIENT span
// and injects traceparent, so the trace starts here. Returns the HTTP status.
func (c *client) do(ctx context.Context, op Op) (status int, err error) {
	ctx, span := c.tracer.Start(ctx, "loadgen "+string(op), trace.WithAttributes(
		attribute.String("loadgen.scenario", c.scenario), attribute.String("loadgen.op", string(op))))
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	switch op {
	case OpList:
		return c.list(ctx)
	case OpView, OpThumbnail:
		id, ok := c.pick(false)
		if !ok {
			return c.list(ctx)
		}
		suffix := "/view"
		if op == OpThumbnail {
			suffix = "/thumbnail"
		}
		return c.get(ctx, fmt.Sprintf(imagesPath+"/%d%s", id, suffix))
	case OpUpload:
		return c.upload(ctx)
	case OpDelete:
		id, ok := c.pick(true)
		if !ok {
			return c.upload(ctx) // nothing of ours to delete yet
		}
		return c.send(ctx, http.MethodDelete, fmt.Sprintf(imagesPath+"/%d", id), nil, "")
	case OpSettings:
		return c.get(ctx, "/api/settings")
	}
	return 0, fmt.Errorf("unknown op %q", op)
}

func (c *client) list(ctx context.Context) (int, error) {
	path := imagesPath
	if c.rnd.IntN(10) < 3 {
		path += "?tags=" + browseTags[c.rnd.IntN(len(browseTags))]
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, http.NoBody)
	if err != nil {
		return 0, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck // Resource cleanup
	var body struct {
		Images []struct {
			ID json.RawMessage `json:"id"`
		} `json:"images"`
	}
	if resp.StatusCode < 300 && json.NewDecoder(resp.Body).Decode(&body) == nil {
		c.mu.Lock()
		for _, im := range body.Images {
			if id, err := strconv.Atoi(strings.Trim(string(im.ID), `"`)); err == nil && len(c.ids) < 500 {
				c.ids = append(c.ids, id)
			}
		}
		c.mu.Unlock()
	}
	return resp.StatusCode, statusErr(resp.StatusCode)
}

func (c *client) upload(ctx context.Context) (int, error) {
	data, name, contentType, err := generateImage(c.rnd)
	if err != nil {
		return 0, err
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Disposition", fmt.Sprintf(`form-data; name="files"; filename=%q`, name))
	hdr.Set("Content-Type", contentType)
	part, err := mw.CreatePart(hdr)
	if err != nil {
		return 0, err
	}
	_, _ = part.Write(data)              //nolint:errcheck // writing into an in-memory bytes.Buffer never fails
	_ = mw.WriteField("tags", "loadgen") //nolint:errcheck // writing into an in-memory bytes.Buffer never fails
	_ = mw.Close()                       //nolint:errcheck // writing into an in-memory bytes.Buffer never fails
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+imagesPath, &buf)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck // Resource cleanup
	var body struct {
		Images []struct {
			ID json.RawMessage `json:"id"`
		} `json:"images"`
	}
	if resp.StatusCode < 300 && json.NewDecoder(resp.Body).Decode(&body) == nil {
		c.mu.Lock()
		for _, im := range body.Images {
			if id, err := strconv.Atoi(strings.Trim(string(im.ID), `"`)); err == nil {
				c.mine = append(c.mine, id)
			}
		}
		c.mu.Unlock()
	}
	return resp.StatusCode, statusErr(resp.StatusCode)
}

func (c *client) get(ctx context.Context, path string) (int, error) {
	return c.send(ctx, http.MethodGet, path, nil, "")
}

func (c *client) send(ctx context.Context, method, path string, body []byte, contentType string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	_, _ = io.Copy(io.Discard, resp.Body) //nolint:errcheck // draining before close; nothing actionable on failure
	_ = resp.Body.Close()                 //nolint:errcheck // Resource cleanup
	return resp.StatusCode, statusErr(resp.StatusCode)
}

// pick returns a known image ID; own=true pops one this run uploaded.
func (c *client) pick(own bool) (int, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if own {
		if len(c.mine) == 0 {
			return 0, false
		}
		id := c.mine[len(c.mine)-1]
		c.mine = c.mine[:len(c.mine)-1]
		return id, true
	}
	if len(c.ids) == 0 {
		return 0, false
	}
	return c.ids[c.rnd.IntN(len(c.ids))], true
}

// statusErr counts 5xx as failures; a 4xx (e.g. an image another client deleted) is not the server failing.
func statusErr(code int) error {
	if code >= 500 {
		return fmt.Errorf("server error %d", code)
	}
	return nil
}
