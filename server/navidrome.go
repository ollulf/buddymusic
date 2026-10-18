package main

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type SubsonicCreds struct {
	User  string `json:"u"`
	Token string `json:"t"`
	Salt  string `json:"s"`
}

func NewCreds(user, password string) SubsonicCreds {
	b := make([]byte, 12)
	rand.Read(b)
	salt := hex.EncodeToString(b)
	sum := md5.Sum([]byte(password + salt))
	return SubsonicCreds{User: user, Token: hex.EncodeToString(sum[:]), Salt: salt}
}

var ErrBadLogin = errors.New("wrong username or password")

type Navidrome struct {
	base   string
	client *http.Client
}

func NewNavidrome(base string) *Navidrome {
	return &Navidrome{base: base, client: &http.Client{Timeout: 15 * time.Second}}
}

type subsonicResponse struct {
	Response struct {
		Status string `json:"status"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		ScanStatus *struct {
			Scanning bool `json:"scanning"`
			Count    int  `json:"count"`
		} `json:"scanStatus"`
	} `json:"subsonic-response"`
}

func (n *Navidrome) call(ctx context.Context, endpoint string, c SubsonicCreds, extra url.Values) (*subsonicResponse, error) {
	q := url.Values{}
	for k, v := range extra {
		q[k] = v
	}
	q.Set("u", c.User)
	q.Set("t", c.Token)
	q.Set("s", c.Salt)
	q.Set("v", "1.16.1")
	q.Set("c", "buddymusic")
	q.Set("f", "json")
	req, err := http.NewRequestWithContext(ctx, "GET", n.base+"/rest/"+endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	res, err := n.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("navidrome %s: http %d", endpoint, res.StatusCode)
	}
	var out subsonicResponse
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("navidrome %s: %w", endpoint, err)
	}
	if out.Response.Status != "ok" {
		if e := out.Response.Error; e != nil && (e.Code == 40 || e.Code == 41) {
			return nil, ErrBadLogin
		}
		if e := out.Response.Error; e != nil {
			return nil, fmt.Errorf("navidrome %s: %d %s", endpoint, e.Code, e.Message)
		}
		return nil, fmt.Errorf("navidrome %s: status %q", endpoint, out.Response.Status)
	}
	return &out, nil
}

func (n *Navidrome) Ping(ctx context.Context, c SubsonicCreds) error {
	_, err := n.call(ctx, "ping.view", c, nil)
	return err
}

func (n *Navidrome) StartScan(ctx context.Context, c SubsonicCreds) error {
	_, err := n.call(ctx, "startScan.view", c, nil)
	return err
}
