package spiffe

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/spiffe/go-spiffe/v2/bundle/x509bundle"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/svid/jwtsvid"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
)

// atomicWrite writes data to path via a tmp file in the same directory
// followed by os.Rename. Same-directory rename is atomic on POSIX, which
// guarantees external readers always see either the old or the new file
// content — never a partial write.
func atomicWrite(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-"+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("atomicWrite: create temp in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("atomicWrite: write %s: %w", tmpName, err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("atomicWrite: chmod %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("atomicWrite: close %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("atomicWrite: rename %s -> %s: %w", tmpName, path, err)
	}
	return nil
}

// mirrorX509Source is the minimal surface of *workloadapi.X509Source that
// the mirror needs. Defining it here lets tests substitute a hand-rolled
// fake, mirroring the seam pattern in workload_x509.go's x509SVIDFetcher.
// *workloadapi.X509Source satisfies this implicitly via structural typing.
type mirrorX509Source interface {
	GetX509SVID() (*x509svid.SVID, error)
	GetX509BundleForTrustDomain(trustDomain spiffeid.TrustDomain) (*x509bundle.Bundle, error)
	Updated() <-chan struct{}
}

// mirrorJWTSource is the minimal surface of *workloadapi.JWTSource that
// the mirror needs. *workloadapi.JWTSource satisfies this implicitly.
type mirrorJWTSource interface {
	FetchJWTSVID(ctx context.Context, params jwtsvid.Params) (*jwtsvid.SVID, error)
}

// mirrorConfig is the immutable configuration for a mirror. JWT may be
// nil; if so, or if JWTAudience is empty, the JWT mirror loop is skipped.
type mirrorConfig struct {
	Dir         string
	X509        mirrorX509Source
	JWT         mirrorJWTSource // nil = no JWT mirror
	JWTAudience string          // ignored if JWT is nil or audience empty
}

// mirror copies SPIFFE credentials from in-memory sources to disk for
// readers that still consume files (e.g. legacy spiffe-helper layouts).
// All filesystem failures are best-effort: errors are logged at WARN and
// never propagate, since the in-memory hot path is the source of truth.
type mirror struct {
	cfg mirrorConfig

	// nextJWTSleep is set by writeJWT and read by runJWT, both on the
	// runJWT goroutine. No mutex needed because only one goroutine
	// touches it (runX509 and the initial-write path don't).
	nextJWTSleep time.Duration
}

// newMirror constructs a mirror from cfg. The mirror does no I/O until
// run() is called.
func newMirror(cfg mirrorConfig) *mirror {
	return &mirror{cfg: cfg}
}

// run blocks until ctx is cancelled AND all spawned goroutines (JWT
// refresh, if configured) have returned. It performs an initial X.509
// write, optionally spawns a JWT refresh goroutine, then blocks on the
// X.509 rotation loop until cancellation. Waiting for the JWT goroutine
// before returning is important so callers can rely on "run returned →
// no more file I/O" — useful for test cleanup and graceful shutdown.
func (m *mirror) run(ctx context.Context) {
	if err := m.writeX509(); err != nil {
		slog.Warn("spiffe.mirror: initial x509 write", "err", err)
	}
	jwtDone := make(chan struct{})
	if m.cfg.JWT != nil && m.cfg.JWTAudience != "" {
		go func() {
			defer close(jwtDone)
			m.runJWT(ctx)
		}()
	} else {
		close(jwtDone)
	}
	m.runX509(ctx)
	<-jwtDone
}

// runX509 subscribes to the X.509 source's Updated channel and writes
// the SVID + bundle on every signal. Blocks until ctx is done.
func (m *mirror) runX509(ctx context.Context) {
	ch := m.cfg.X509.Updated()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ch:
			if err := m.writeX509(); err != nil {
				slog.Warn("spiffe.mirror: x509 rotation write", "err", err)
			}
		}
	}
}

// runJWT loops calling writeJWT and sleeping until the next refresh time
// (90% of token lifetime) or ctx cancellation. On error, retries every
// 30s.
func (m *mirror) runJWT(ctx context.Context) {
	const errRetry = 30 * time.Second
	for {
		sleep := errRetry
		if err := m.writeJWT(ctx); err == nil {
			sleep = m.nextJWTSleep
		} else {
			slog.Warn("spiffe.mirror: jwt write", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(sleep):
		}
	}
}

// writeX509 fetches the latest X.509 SVID and bundle, encodes them as
// PEM, and writes svid.pem / svid_key.pem / svid_bundle.pem atomically.
// Returns the first error encountered; partial writes are possible if a
// later step fails after an earlier success (acceptable: the next
// rotation overwrites).
func (m *mirror) writeX509() error {
	svid, err := m.cfg.X509.GetX509SVID()
	if err != nil {
		return fmt.Errorf("GetX509SVID: %w", err)
	}
	if svid == nil || len(svid.Certificates) == 0 {
		return errors.New("X509 SVID has no certificates")
	}

	// Cert chain (leaf-first, per X.509 SVID convention).
	var certPEM []byte
	for _, c := range svid.Certificates {
		certPEM = append(certPEM, pem.EncodeToMemory(&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: c.Raw,
		})...)
	}
	if err := atomicWrite(filepath.Join(m.cfg.Dir, "svid.pem"), certPEM, 0o644); err != nil {
		return fmt.Errorf("write svid.pem: %w", err)
	}

	// Private key (PKCS#8).
	keyDER, err := x509.MarshalPKCS8PrivateKey(svid.PrivateKey)
	if err != nil {
		return fmt.Errorf("MarshalPKCS8PrivateKey: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	if err := atomicWrite(filepath.Join(m.cfg.Dir, "svid_key.pem"), keyPEM, 0o600); err != nil {
		return fmt.Errorf("write svid_key.pem: %w", err)
	}

	// Bundle for the workload's own trust domain.
	bundle, err := m.cfg.X509.GetX509BundleForTrustDomain(svid.ID.TrustDomain())
	if err != nil {
		return fmt.Errorf("GetX509BundleForTrustDomain: %w", err)
	}
	var bundlePEM []byte
	for _, c := range bundle.X509Authorities() {
		bundlePEM = append(bundlePEM, pem.EncodeToMemory(&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: c.Raw,
		})...)
	}
	if err := atomicWrite(filepath.Join(m.cfg.Dir, "svid_bundle.pem"), bundlePEM, 0o644); err != nil {
		return fmt.Errorf("write svid_bundle.pem: %w", err)
	}
	return nil
}

// writeJWT fetches a fresh JWT SVID for the configured audience, writes
// it to jwt_svid.token, and updates m.nextJWTSleep to 90% of the token's
// remaining lifetime (clamped to a 100ms minimum so a near-expired token
// can't drive a tight loop).
func (m *mirror) writeJWT(ctx context.Context) error {
	const minRefresh = 100 * time.Millisecond
	svid, err := m.cfg.JWT.FetchJWTSVID(ctx, jwtsvid.Params{Audience: m.cfg.JWTAudience})
	if err != nil {
		return fmt.Errorf("FetchJWTSVID: %w", err)
	}
	tokenPath := filepath.Join(m.cfg.Dir, "jwt_svid.token")
	if err := atomicWrite(tokenPath, []byte(svid.Marshal()), 0o644); err != nil {
		return fmt.Errorf("write jwt_svid.token: %w", err)
	}
	sleep := time.Until(svid.Expiry) * 9 / 10
	if sleep < minRefresh {
		sleep = minRefresh
	}
	m.nextJWTSleep = sleep
	return nil
}
