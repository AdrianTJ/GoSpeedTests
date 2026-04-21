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
	)

	allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	
	// Create the browser instance
	browserCtx, _ := chromedp.NewContext(allocCtx)
	
	// Start the browser
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
