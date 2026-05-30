package handler

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/service"
)

const (
	licenseServerURLSetting  = "license.server_url"
	licenseHMACSecretSetting = "license.hmac_secret"
	licenseDeviceIDSetting   = "license.device_id"
	licenseDeviceNameSetting = "license.device_name"
)

type licenseActivateReq struct {
	Key        string `json:"key" binding:"required"`
	DeviceID   string `json:"device_id"`
	DeviceName string `json:"device_name"`
}

type licenseServerSignedResp struct {
	Valid           bool    `json:"valid"`
	LicenseType     string  `json:"license_type"`
	ExpiryDate      *string `json:"expiry_date"`
	MaxDevices      int     `json:"max_devices"`
	MaxUsers        *int    `json:"max_users"`
	DaysRemaining   *int    `json:"days_remaining"`
	NextHeartbeat   string  `json:"next_heartbeat"`
	Signature       string  `json:"signature"`
	LegacySignature bool    `json:"-"`
}

type licenseServerStatusResp struct {
	Valid          bool    `json:"valid"`
	LicenseType    *string `json:"license_type"`
	ExpiryDate     *string `json:"expiry_date"`
	MaxUsers       *int    `json:"max_users"`
	UnlimitedUsers bool    `json:"unlimited_users"`
	DaysRemaining  *int    `json:"days_remaining"`
	DeviceName     string  `json:"device_name"`
	IsActive       bool    `json:"is_active"`
}

func licenseActivateHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req licenseActivateReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		client, err := newLicenseClient(c.Request.Context(), svc)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		deviceID, err := ensureLicenseDeviceID(c.Request.Context(), svc, req.DeviceID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		deviceName := strings.TrimSpace(req.DeviceName)
		if deviceName == "" {
			deviceName = defaultLicenseDeviceName()
		}
		_ = svc.Repo.Setting.Set(c.Request.Context(), licenseDeviceNameSetting, deviceName)

		payload := map[string]any{
			"key":         strings.TrimSpace(req.Key),
			"fingerprint": deviceID,
			"device_name": deviceName,
			"instance_id": deviceID,
		}
		var upstream licenseServerSignedResp
		if err := client.post(c.Request.Context(), "/api/v1/activate", payload, &upstream); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		if err := client.verifySigned(&upstream); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		state := licenseStateFromSigned(upstream, deviceID, deviceName)
		if err := persistLicenseState(c.Request.Context(), svc, state); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, licenseActivationView(state))
	}
}

func licenseStatusHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		state, _ := loadLicenseState(c.Request.Context(), svc)
		client, err := newLicenseClient(c.Request.Context(), svc)
		if err == nil {
			deviceID, idErr := ensureLicenseDeviceID(c.Request.Context(), svc, "")
			if idErr == nil {
				var upstream licenseServerStatusResp
				if getErr := client.get(c.Request.Context(), "/api/v1/status/"+url.PathEscape(deviceID), &upstream); getErr == nil && upstream.Valid {
					state.Valid = upstream.Valid
					if upstream.LicenseType != nil {
						state.LicenseType = *upstream.LicenseType
					}
					if upstream.ExpiryDate != nil {
						state.ExpiryDate = *upstream.ExpiryDate
					}
					state.MaxUsers = upstream.MaxUsers
					state.UnlimitedUsers = upstream.UnlimitedUsers
					state.DaysRemaining = upstream.DaysRemaining
					if upstream.DeviceName != "" {
						state.DeviceName = upstream.DeviceName
					}
					state.DeviceID = deviceID
					state.UpdatedAt = time.Now().Format(time.RFC3339)
					_ = persistLicenseState(c.Request.Context(), svc, state)
				} else if getErr == nil && !upstream.Valid {
					state.Valid = false
					_ = persistLicenseState(c.Request.Context(), svc, state)
				}
			}
		}
		active := state.Valid && !licenseStateExpired(state.ExpiryDate)
		c.JSON(http.StatusOK, gin.H{
			"active":          active,
			"message":         licenseStatusMessage(active, err),
			"max_users":       licenseStatusMaxUsers(state),
			"unlimited_users": state.Valid && !licenseStateExpired(state.ExpiryDate) && state.UnlimitedUsers,
			"activation":      licenseActivationView(state),
		})
	}
}

func licenseHeartbeatHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		client, err := newLicenseClient(c.Request.Context(), svc)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		deviceID, err := ensureLicenseDeviceID(c.Request.Context(), svc, "")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		var upstream licenseServerSignedResp
		if err := client.post(c.Request.Context(), "/api/v1/heartbeat", map[string]any{
			"fingerprint": deviceID,
			"instance_id": deviceID,
		}, &upstream); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		if err := client.verifySigned(&upstream); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		deviceName, _ := svc.Repo.Setting.Get(c.Request.Context(), licenseDeviceNameSetting)
		state := licenseStateFromSigned(upstream, deviceID, deviceName)
		if err := persistLicenseState(c.Request.Context(), svc, state); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, licenseActivationView(state))
	}
}

type licenseClient struct {
	baseURL    string
	hmacSecret string
	httpClient *http.Client
}

func newLicenseClient(ctx context.Context, svc *service.Container) (*licenseClient, error) {
	baseURL, _ := svc.Repo.Setting.Get(ctx, licenseServerURLSetting)
	if strings.TrimSpace(baseURL) == "" {
		baseURL = svc.Cfg.License.ServerURL
	}
	secret, _ := svc.Repo.Setting.Get(ctx, licenseHMACSecretSetting)
	if strings.TrimSpace(secret) == "" {
		secret = svc.Cfg.License.HMACSecret
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, errors.New("license server url not configured")
	}
	return &licenseClient{
		baseURL:    baseURL,
		hmacSecret: strings.TrimSpace(secret),
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}, nil
}

func (c *licenseClient) post(ctx context.Context, path string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, out)
}

func (c *licenseClient) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	return c.do(req, out)
}

func (c *licenseClient) do(req *http.Request, out any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var er struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(data, &er)
		if er.Message != "" {
			return fmt.Errorf("license server: %s", er.Message)
		}
		if er.Error != "" {
			return fmt.Errorf("license server: %s", er.Error)
		}
		return fmt.Errorf("license server http %d", resp.StatusCode)
	}
	return json.Unmarshal(data, out)
}

func (c *licenseClient) verifySigned(resp *licenseServerSignedResp) error {
	if c.hmacSecret == "" {
		return nil
	}
	unsigned := struct {
		Valid         bool    `json:"valid"`
		LicenseType   string  `json:"license_type"`
		ExpiryDate    *string `json:"expiry_date"`
		MaxDevices    int     `json:"max_devices"`
		MaxUsers      *int    `json:"max_users"`
		DaysRemaining *int    `json:"days_remaining"`
		NextHeartbeat string  `json:"next_heartbeat"`
	}{
		Valid:         resp.Valid,
		LicenseType:   resp.LicenseType,
		ExpiryDate:    resp.ExpiryDate,
		MaxDevices:    resp.MaxDevices,
		MaxUsers:      resp.MaxUsers,
		DaysRemaining: resp.DaysRemaining,
		NextHeartbeat: resp.NextHeartbeat,
	}
	payload, err := json.Marshal(unsigned)
	if err != nil {
		return err
	}
	mac := hmac.New(sha256.New, []byte(c.hmacSecret))
	_, _ = mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(resp.Signature)) {
		if c.verifyLegacySigned(*resp) {
			resp.LegacySignature = true
			return nil
		}
		return errors.New("license server signature verification failed")
	}
	return nil
}

func (c *licenseClient) verifyLegacySigned(resp licenseServerSignedResp) bool {
	unsigned := struct {
		Valid         bool    `json:"valid"`
		LicenseType   string  `json:"license_type"`
		ExpiryDate    *string `json:"expiry_date"`
		MaxDevices    int     `json:"max_devices"`
		DaysRemaining *int    `json:"days_remaining"`
		NextHeartbeat string  `json:"next_heartbeat"`
	}{
		Valid:         resp.Valid,
		LicenseType:   resp.LicenseType,
		ExpiryDate:    resp.ExpiryDate,
		MaxDevices:    resp.MaxDevices,
		DaysRemaining: resp.DaysRemaining,
		NextHeartbeat: resp.NextHeartbeat,
	}
	payload, err := json.Marshal(unsigned)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(c.hmacSecret))
	_, _ = mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(resp.Signature))
}

func ensureLicenseDeviceID(ctx context.Context, svc *service.Container, candidate string) (string, error) {
	if strings.TrimSpace(candidate) != "" {
		return strings.TrimSpace(candidate), svc.Repo.Setting.Set(ctx, licenseDeviceIDSetting, strings.TrimSpace(candidate))
	}
	existing, err := svc.Repo.Setting.Get(ctx, licenseDeviceIDSetting)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(existing) != "" {
		return strings.TrimSpace(existing), nil
	}
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	id := "msgo-" + hex.EncodeToString(buf[:])
	return id, svc.Repo.Setting.Set(ctx, licenseDeviceIDSetting, id)
}

func defaultLicenseDeviceName() string {
	host, _ := os.Hostname()
	if strings.TrimSpace(host) == "" {
		return "MediaStationGo Server"
	}
	return "MediaStationGo - " + host
}

func licenseStateFromSigned(resp licenseServerSignedResp, deviceID, deviceName string) service.LicenseActivationState {
	expiry := ""
	if resp.ExpiryDate != nil {
		expiry = *resp.ExpiryDate
	}
	return service.LicenseActivationState{
		Valid:          resp.Valid,
		LicenseType:    resp.LicenseType,
		ExpiryDate:     expiry,
		MaxDevices:     resp.MaxDevices,
		MaxUsers:       resp.MaxUsers,
		UnlimitedUsers: !resp.LegacySignature && resp.MaxUsers == nil,
		DaysRemaining:  resp.DaysRemaining,
		NextHeartbeat:  resp.NextHeartbeat,
		DeviceID:       deviceID,
		DeviceName:     deviceName,
		UpdatedAt:      time.Now().Format(time.RFC3339),
	}
}

func licenseStatusMaxUsers(state service.LicenseActivationState) any {
	active := state.Valid && !licenseStateExpired(state.ExpiryDate)
	if active {
		if state.UnlimitedUsers {
			return nil
		}
		if state.MaxUsers != nil && *state.MaxUsers > 0 {
			return *state.MaxUsers
		}
		return service.LicensedUserLimit
	}
	return service.OpenSourceUserLimit
}

func persistLicenseState(ctx context.Context, svc *service.Container, state service.LicenseActivationState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return svc.Repo.Setting.Set(ctx, service.LicenseSettingActivation, string(data))
}

func loadLicenseState(ctx context.Context, svc *service.Container) (service.LicenseActivationState, error) {
	raw, err := svc.Repo.Setting.Get(ctx, service.LicenseSettingActivation)
	if err != nil || raw == "" {
		return service.LicenseActivationState{}, err
	}
	var state service.LicenseActivationState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return service.LicenseActivationState{}, err
	}
	return state, nil
}

func licenseActivationView(state service.LicenseActivationState) gin.H {
	updatedAt := state.UpdatedAt
	if strings.TrimSpace(updatedAt) == "" {
		updatedAt = time.Now().Format(time.RFC3339)
	}
	return gin.H{
		"id":              state.DeviceID,
		"key_id":          state.LicenseType,
		"device_id":       state.DeviceID,
		"device_name":     state.DeviceName,
		"plan":            state.LicenseType,
		"max_activations": state.MaxDevices,
		"max_users":       state.MaxUsers,
		"unlimited_users": state.UnlimitedUsers,
		"expires_at":      emptyAsNil(state.ExpiryDate),
		"valid":           state.Valid && !licenseStateExpired(state.ExpiryDate),
		"heartbeat_at":    updatedAt,
		"created_at":      updatedAt,
	}
}

func licenseStatusMessage(active bool, clientErr error) string {
	if active {
		return "已激活"
	}
	if clientErr != nil && !strings.Contains(clientErr.Error(), "not configured") {
		return clientErr.Error()
	}
	return "开源版：最多 20 个用户"
}

func licenseStateExpired(expiry string) bool {
	if expiry == "" {
		return false
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, expiry); err == nil {
			return time.Now().After(t)
		}
	}
	return false
}

func emptyAsNil(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}
