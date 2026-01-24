package integration

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/api"
	proxyconfig "github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	sdkaccess "github.com/router-for-me/CLIProxyAPI/v6/sdk/access"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/api/handlers"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
)

var (
	// testServer is the httptest server running the proxy
	testServer *httptest.Server
	// testBaseURL is the URL of the test server
	testBaseURL string
	// useMockLLM indicates whether to use mock responses (true) or real API (false)
	useMockLLM bool
	// budget tracks API spending in real API mode
	budget *BudgetTracker
	// mockTransport is the mock RoundTripper for intercepting LLM calls
	mockTransport *MockLLMRoundTripper
	// coreManager is the auth manager used by tests
	coreManager *auth.Manager
)

// TestMain sets up the test environment and runs all tests.
func TestMain(m *testing.M) {
	// Set gin to test mode
	gin.SetMode(gin.TestMode)

	// Check for real API mode
	budgetStr := os.Getenv("TEST_REAL_API_BUDGET")
	if budgetStr != "" {
		useMockLLM = false
		budget = NewBudgetTracker(budgetStr)
	} else {
		useMockLLM = true
		mockTransport = NewMockLLMRoundTripper()
	}

	// Start test server
	testServer, coreManager = startTestServer()
	testBaseURL = testServer.URL

	// Run tests
	code := m.Run()

	// Cleanup
	testServer.Close()

	// Print budget summary if using real API
	if budget != nil {
		println("Budget Summary:", budget.Summary())
	}

	os.Exit(code)
}

// startTestServer creates and starts the test proxy server.
func startTestServer() (*httptest.Server, *auth.Manager) {
	tmpDir := os.TempDir()
	authDir := filepath.Join(tmpDir, "integration-test-auth")
	if err := os.MkdirAll(authDir, 0o700); err != nil {
		panic("failed to create auth dir: " + err.Error())
	}

	cfg := &proxyconfig.Config{
		SDKConfig: sdkconfig.SDKConfig{
			APIKeys: []string{"test-key"},
		},
		Port:                   0,
		AuthDir:                authDir,
		Debug:                  true,
		LoggingToFile:          false,
		UsageStatisticsEnabled: false,
	}

	// Create auth manager with mock RoundTripper if in mock mode
	var rtProvider auth.RoundTripperProvider
	if useMockLLM && mockTransport != nil {
		rtProvider = &mockRoundTripperProvider{transport: mockTransport}
	}

	authManager := auth.NewManager(nil, nil, nil)
	if rtProvider != nil {
		authManager.SetRoundTripperProvider(rtProvider)
	}
	authManager.SetConfig(cfg)

	accessManager := sdkaccess.NewManager()

	// Capture the gin.Engine via router configurator
	var engine *gin.Engine
	routerCapture := api.WithRouterConfigurator(func(e *gin.Engine, _ *handlers.BaseAPIHandler, _ *proxyconfig.Config) {
		engine = e
	})

	configPath := filepath.Join(tmpDir, "config.yaml")
	_ = api.NewServer(cfg, authManager, accessManager, configPath, routerCapture)

	if engine == nil {
		panic("failed to capture gin engine from server")
	}

	// Create httptest server using the captured engine
	ts := httptest.NewServer(engine)
	return ts, authManager
}

// mockRoundTripperProvider implements auth.RoundTripperProvider for testing.
type mockRoundTripperProvider struct {
	transport http.RoundTripper
}

// RoundTripperFor returns the mock transport for all auth entries.
func (p *mockRoundTripperProvider) RoundTripperFor(a *auth.Auth) http.RoundTripper {
	return p.transport
}

// GetMockTransport returns the mock transport for test configuration.
// Returns nil if running in real API mode.
func GetMockTransport() *MockLLMRoundTripper {
	return mockTransport
}

// ResetMockTransport clears recorded requests and custom responses.
func ResetMockTransport() {
	if mockTransport != nil {
		mockTransport.ClearRequests()
		mockTransport.ClearCustomResponses()
	}
}
