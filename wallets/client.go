package wallets

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/ethereum/go-ethereum/common"
	"github.com/zeebo/errs"
	"io/ioutil"
	"net/http"
)

// WalletClient is a REST client for wallet endpoints.
type WalletClient struct {
	Secret   string
	Endpoint string
}

// NewWalletClient creates a new wallet client from HTTP endpoint address and secret.
func NewWalletClient(endpoint string, secret string) *WalletClient {
	return &WalletClient{
		Endpoint: endpoint,
		Secret:   secret,
	}
}

// PersistAddresses sends claimable generated addresses to the backend.
func (w *WalletClient) PersistAddresses(ctx context.Context, addresses []common.Address) error {
	return w.httpPost(ctx, w.Endpoint+"/api/v0/wallets/", addresses)
}

func (w *WalletClient) httpPost(ctx context.Context, url string, request interface{}) (err error) {
	defer mon.Task()(&ctx)(&err)

	body, err := json.Marshal(request)
	if err != nil {
		return errs.Wrap(err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return errs.Wrap(err)
	}
	req.Header.Add("STORJSCAN_API_KEY", w.Secret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errs.Wrap(err)
	}

	if resp.StatusCode >= 300 {
		body, err := ioutil.ReadAll(resp.Body)
		return errs.Combine(
			errs.New("HTTP error %d for %s, %s", resp.StatusCode, url, string(body)), err, resp.Body.Close())
	}
	return err
}
