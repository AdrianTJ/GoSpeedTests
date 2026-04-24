package chrome

import (
	"context"
	"log"
	"sync"

	"github.com/chromedp/chromedp"
)

type Manager struct {
	allocCtx context.Context
	cancel   context.CancelFunc
	browser  context.Context
	mu       sync.Mutex
}

func NewManager() *Manager {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.NoSandbox,
		chromedp.DisableGPU,
		chromedp.Headless,
		// Force a consistent window size and User-Agent to ensure paints fire
		chromedp.WindowSize(1920, 1080),
		chromedp.UserAgent("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"),
	)

	allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	
	// Create the master browser context
	browserCtx, _ := chromedp.NewContext(allocCtx)
	
	// Start the browser to ensure it's ready
	if err := chromedp.Run(browserCtx); err != nil {
		log.Printf("Failed to start browser: %v", err)
	}

	return &Manager{
		allocCtx: allocCtx,
		cancel:   cancel,
		browser:  browserCtx,
	}
}

func (m *Manager) NewContext(ctx context.Context) (context.Context, context.CancelFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	// Create a new tab in the existing browser
	return chromedp.NewContext(m.browser)
}

func (m *Manager) Close() {
	m.cancel()
}
