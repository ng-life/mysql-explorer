package server

import (
	"context"
	"github.com/ng-life/mysql-explorer/internal/config"
	"github.com/ng-life/mysql-explorer/internal/mysql"
	"log"
	"net"
	"net/http"
	"time"
)

// Run serves HTTP until cancellation, then closes MySQL sessions.
func Run(ctx context.Context, c config.Config) error {
	pool := mysql.NewPool(c.Clusters)
	defer pool.Close()
	pool.Preload(ctx)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	listener, err := net.Listen("tcp", c.Listen)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: New(c, pool).Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	log.Printf("MySQL Explorer is ready at %s", listener.Addr())
	select {
	case err := <-done:
		if err != http.ErrServerClosed {
			return err
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			server.Close()
			return err
		}
	}
	return nil
}
