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
	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/service"
)

const (
	licenseServerURLSetting  = "license.server_url"
	licenseHMACSecretSetting = "license.hmac_secret" // #nosec G101 -- setting key name, not the HMAC secret value.
	licenseDeviceIDSetting   = "license.device_id"
	licenseDeviceNameSetting = "license.device_name"

	licenseHeartbeatInterval      = 12 * time.Hour
	licenseHeartbeatCheckInterval = 30 * time.Minute
	licenseHeartbeatStartupDelay  = 2 * time.Minute
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
	MaxDevices     int     `json:"max_devices"`
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
		state.LicenseKey = strings.TrimSpace(req.Key)
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
			deviceID, idErr := ensureLicenseDeviceID(c.Request.Context(), svc, state.DeviceID)
			if idErr == nil {
				deviceName, _ := svc.Repo.Setting.Get(c.Request.Context(), licenseDeviceNameSetting)
				if strings.TrimSpace(deviceName) == "" {
					deviceName = defaultLicenseDeviceName()
					_ = svc.Repo.Setting.Set(c.Request.Context(), licenseDeviceNameSetting, deviceName)
				}
				var signed licenseServerSignedResp
				if heartbeatErr := client.post(c.Request.Context(), "/api/v1/heartbeat", licenseHeartbeatPayload(state, deviceID, deviceName), &signed); heartbeatErr == nil && client.verifySigned(&signed) == nil {
					nextState := licenseStateFromSigned(signed, deviceID, deviceName)
					nextState.LicenseKey = state.LicenseKey
					state = nextState
					_ = persistLicenseState(c.Request.Context(), svc, state)
				} else {
					var upstream licenseServerStatusResp
					if getErr := client.get(c.Request.Context(), "/api/v1/status/"+url.PathEscape(deviceID), &upstream); getErr == nil && upstream.Valid {
						applyLicenseStatus(&state, upstream, deviceID)
						_ = persistLicenseState(c.Request.Context(), svc, state)
					} else if getErr == nil && !upstream.Valid {
						state.Valid = false
						_ = persistLicenseState(c.Request.Context(), svc, state)
					}
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
		state, err := sendLicenseHeartbeat(c.Request.Context(), svc)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, licenseActivationView(state))
	}
}

// RunLicenseHeartbeatLoop keeps the license server aware of active deployments.
// The loop checks periodically, but only sends when the last stored heartbeat is
// older than licenseHeartbeatInterval.
func RunLicenseHeartbeatLoop(ctx context.Context, svc *service.Container) {
	if svc == nil {
		return
	}
	run := func() {
		state, sent, err := maybeSendLicenseHeartbeat(ctx, svc, licenseHeartbeatInterval)
		if err != nil {
			if svc.Log != nil {
				svc.Log.Warn("license heartbeat failed", zap.Error(err))
			}
			return
		}
		if sent && svc.Log != nil {
			svc.Log.Info("license heartbeat sent", zap.String("device_id", state.DeviceID))
		}
	}

	timer := time.NewTimer(licenseHeartbeatStartupDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
		run()
	}

	ticker := time.NewTicker(licenseHeartbeatCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}

func maybeSendLicenseHeartbeat(ctx context.Context, svc *service.Container, interval time.Duration) (service.LicenseActivationState, bool, error) {
	state, err := loadLicenseState(ctx, svc)
	if err != nil {
		return state, false, nil
	}
	if !licenseHeartbeatEligible(state) || !licenseHeartbeatDue(state, interval) {
		return state, false, nil
	}
	next, err := sendLicenseHeartbeat(ctx, svc)
	if err != nil {
		return state, false, err
	}
	return next, true, nil
}

func licenseHeartbeatEligible(state service.LicenseActivationState) bool {
	return strings.TrimSpace(state.LicenseKey) != "" || state.Valid
}

func licenseHeartbeatDue(state service.LicenseActivationState, interval time.Duration) bool {
	if interval <= 0 {
		return true
	}
	updatedAt := strings.TrimSpace(state.UpdatedAt)
	if updatedAt == "" {
		return true
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, updatedAt); err == nil {
			return time.Since(t) >= interval
		}
	}
	return true
}

func sendLicenseHeartbeat(ctx context.Context, svc *service.Container) (service.LicenseActivationState, error) {
	client, err := newLicenseClient(ctx, svc)
	if err != nil {
		return service.LicenseActivationState{}, err
	}
	oldState, _ := loadLicenseState(ctx, svc)
	deviceID, err := ensureLicenseDeviceID(ctx, svc, oldState.DeviceID)
	if err != nil {
		return service.LicenseActivationState{}, err
	}
	deviceName, _ := svc.Repo.Setting.Get(ctx, licenseDeviceNameSetting)
	if strings.TrimSpace(deviceName) == "" {
		deviceName = defaultLicenseDeviceName()
		_ = svc.Repo.Setting.Set(ctx, licenseDeviceNameSetting, deviceName)
	}
	var upstream licenseServerSignedResp
	if err := client.post(ctx, "/api/v1/heartbeat", licenseHeartbeatPayload(oldState, deviceID, deviceName), &upstream); err != nil {
		return service.LicenseActivationState{}, err
	}
	if err := client.verifySigned(&upstream); err != nil {
		return service.LicenseActivationState{}, err
	}
	state := licenseStateFromSigned(upstream, deviceID, deviceName)
	state.LicenseKey = oldState.LicenseKey
	if err := persistLicenseState(ctx, svc, state); err != nil {
		return service.LicenseActivationState{}, err
	}
	return state, nil
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

func licenseHeartbeatPayload(state service.LicenseActivationState, deviceID, deviceName string) map[string]any {
	payload := map[string]any{
		"fingerprint": deviceID,
		"instance_id": deviceID,
		"device_name": deviceName,
	}
	if key := strings.TrimSpace(state.LicenseKey); key != "" {
		payload["key"] = key
	}
	return payload
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

func applyLicenseStatus(state *service.LicenseActivationState, upstream licenseServerStatusResp, deviceID string) {
	state.Valid = upstream.Valid
	if upstream.LicenseType != nil {
		state.LicenseType = *upstream.LicenseType
	}
	if upstream.ExpiryDate != nil {
		state.ExpiryDate = *upstream.ExpiryDate
	} else {
		state.ExpiryDate = ""
	}
	if upstream.MaxDevices > 0 {
		state.MaxDevices = upstream.MaxDevices
	}
	state.MaxUsers = upstream.MaxUsers
	state.UnlimitedUsers = upstream.UnlimitedUsers
	state.DaysRemaining = upstream.DaysRemaining
	if upstream.DeviceName != "" {
		state.DeviceName = upstream.DeviceName
	}
	state.DeviceID = deviceID
	state.UpdatedAt = time.Now().Format(time.RFC3339)
}

func persistLicenseState(ctx context.Context, svc *service.Container, state service.LicenseActivationState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if strings.TrimSpace(state.DeviceID) != "" {
		_ = svc.Repo.Setting.Set(ctx, licenseDeviceIDSetting, strings.TrimSpace(state.DeviceID))
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
