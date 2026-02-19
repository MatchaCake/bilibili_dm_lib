package dm

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

// wbiMixinKey table — fixed by Bilibili, used to derive signing key from img+sub keys.
var mixinKeyTable = []int{
	46, 47, 18, 2, 53, 8, 23, 32, 15, 50, 10, 31, 58, 3, 45, 35,
	27, 43, 5, 49, 33, 9, 42, 19, 29, 28, 14, 39, 12, 38, 41, 13,
	37, 48, 7, 16, 24, 55, 40, 61, 26, 17, 0, 1, 60, 51, 52, 25,
	22, 44, 56, 30, 20, 36, 11, 21, 4, 34, 54, 57, 59, 6,
}

// getWbiKeys fetches the wbi img_key and sub_key from the nav API.
func getWbiKeys(ctx context.Context, hc *http.Client, cookies string) (imgKey, subKey string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.bilibili.com/x/web-interface/nav", nil)
	if err != nil {
		return "", "", err
	}
	setCommonHeaders(req, cookies)

	resp, err := hc.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("nav request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("read nav response: %w", err)
	}

	var result struct {
		Code int `json:"code"`
		Data struct {
			WbiImg struct {
				ImgURL string `json:"img_url"`
				SubURL string `json:"sub_url"`
			} `json:"wbi_img"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", "", fmt.Errorf("parse nav: %w", err)
	}

	// Extract key from URL: take filename without extension
	// e.g. "https://i0.hdslb.com/bfs/wbi/7cd084941338484aae1ad9425b84077c.png" -> "7cd084941338484aae1ad9425b84077c"
	imgKey = strings.TrimSuffix(path.Base(result.Data.WbiImg.ImgURL), path.Ext(result.Data.WbiImg.ImgURL))
	subKey = strings.TrimSuffix(path.Base(result.Data.WbiImg.SubURL), path.Ext(result.Data.WbiImg.SubURL))
	return imgKey, subKey, nil
}

// getMixinKey derives the signing key from img_key + sub_key using the mixin table.
func getMixinKey(imgKey, subKey string) string {
	raw := imgKey + subKey
	var key strings.Builder
	for _, idx := range mixinKeyTable {
		if idx < len(raw) {
			key.WriteByte(raw[idx])
		}
	}
	s := key.String()
	if len(s) > 32 {
		s = s[:32]
	}
	return s
}

// signWbi signs query parameters with wbi. Returns the signed query string.
func signWbi(params map[string]string, mixinKey string) string {
	// Add wts (timestamp)
	params["wts"] = strconv.FormatInt(time.Now().Unix(), 10)

	// Sort keys
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Build query string (sanitized: remove special chars)
	var query strings.Builder
	for i, k := range keys {
		if i > 0 {
			query.WriteByte('&')
		}
		query.WriteString(url.QueryEscape(k))
		query.WriteByte('=')
		query.WriteString(url.QueryEscape(sanitizeWbiValue(params[k])))
	}
	queryStr := query.String()

	// Calculate w_rid = md5(query + mixin_key)
	h := md5.New()
	h.Write([]byte(queryStr + mixinKey))
	wRid := hex.EncodeToString(h.Sum(nil))

	return queryStr + "&w_rid=" + wRid
}

// sanitizeWbiValue removes characters that Bilibili doesn't allow in wbi-signed values.
func sanitizeWbiValue(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r != '!' && r != '\'' && r != '(' && r != ')' && r != '*' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
