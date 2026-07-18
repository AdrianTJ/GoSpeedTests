package chrome

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/chromedp/chromedp"
)

type Manager struct {
	allocCtx context.Context
	cancel   context.CancelFunc
	browser  context.Context
	startErr error // non-nil when the browser failed to launch
	mu       sync.Mutex
}

func NewManager() *Manager {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.DisableGPU,
		chromedp.Headless,
		// Force a consistent window size and User-Agent to ensure paints fire
		chromedp.WindowSize(1920, 1080),
		chromedp.UserAgent("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"),
	)

	// The Chrome sandbox is left ENABLED by default. It is a critical defense:
	// a compromised renderer (e.g. via a malicious page in a browser/vitals
	// tier) cannot escape to the host. Only disable it deliberately, in an
	// already-isolated environment that cannot support the sandbox (e.g. a
	// container without the setuid helper or user namespaces).
	if os.Getenv("LOADSTAR_CHROME_NO_SANDBOX") == "true" {
		slog.Warn("Chrome sandbox DISABLED via LOADSTAR_CHROME_NO_SANDBOX; a browser exploit could escape to the host. Only use this in a trusted, network-isolated deployment.")
		opts = append(opts, chromedp.NoSandbox)
	} else if os.Geteuid() == 0 {
		// chromedp auto-appends --no-sandbox when running as root (Chrome
		// refuses to start otherwise), so the sandbox is effectively OFF here
		// regardless of the flag above. Surface that instead of leaving it
		// silent; run as a non-root user to keep the sandbox enabled.
		slog.Warn("Running as root: Chrome's sandbox is auto-disabled by chromedp (--no-sandbox). Run as a non-root user to keep it enabled.")
	}

	allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)

	// Create the master browser context
	browserCtx, _ := chromedp.NewContext(allocCtx)

	// Start the browser to ensure it's ready. If it can't launch (e.g. no
	// Chrome binary on the host), remember the error: handing out tabs from a
	// never-started browser makes chromedp's tab cancel block forever, wedging
	// a worker permanently.
	var startErr error
	if err := chromedp.Run(browserCtx); err != nil {
		slog.Error("Failed to start browser", "error", err)
		startErr = fmt.Errorf("headless browser unavailable: %w", err)
	}

	return &Manager{
		allocCtx: allocCtx,
		cancel:   cancel,
		browser:  browserCtx,
		startErr: startErr,
	}
}

// NewContext returns a fresh tab context bound to the caller's ctx. It fails
// fast with an error when the shared browser never started, so callers report
// a clear tier failure instead of hanging on a dead browser.
func (m *Manager) NewContext(ctx context.Context) (context.Context, context.CancelFunc, error) {
	if m.startErr != nil {
		return nil, nil, m.startErr
	}

	m.mu.Lock()
	// Create a new tab in the existing browser. The tab derives from the
	// shared browser context, not from ctx, so we must propagate the
	// caller's cancellation/deadline explicitly.
	tabCtx, tabCancel := chromedp.NewContext(m.browser)
	m.mu.Unlock()

	stop := context.AfterFunc(ctx, tabCancel)
	return tabCtx, func() {
		stop()
		tabCancel()
	}, nil
}

func (m *Manager) Close() {
	m.cancel()
}
