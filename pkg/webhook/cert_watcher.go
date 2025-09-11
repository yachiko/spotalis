/*
Copyright 2024 The Spotalis Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package webhook

import (
	"context"
	"crypto/tls"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
	ctrl "sigs.k8s.io/controller-runtime"
)

// CertificateWatcher watches certificate files for changes and reloads them
type CertificateWatcher struct {
	certPath string
	keyPath  string
	onReload func(tls.Certificate)
	watcher  *fsnotify.Watcher
}

// NewCertificateWatcher creates a new certificate watcher
func NewCertificateWatcher(certPath, keyPath string, onReload func(tls.Certificate)) *CertificateWatcher {
	return &CertificateWatcher{
		certPath: certPath,
		keyPath:  keyPath,
		onReload: onReload,
	}
}

// Start starts the certificate watcher
func (cw *CertificateWatcher) Start(ctx context.Context) error {
	log := ctrl.Log.WithName("cert-watcher")

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	cw.watcher = watcher

	// Watch certificate directory
	certDir := filepath.Dir(cw.certPath)
	if err := watcher.Add(certDir); err != nil {
		return err
	}

	log.Info("Started certificate watcher", "cert-path", cw.certPath, "key-path", cw.keyPath)

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}

			if event.Op&fsnotify.Write == fsnotify.Write {
				if event.Name == cw.certPath || event.Name == cw.keyPath {
					log.Info("Certificate file changed, reloading", "file", event.Name)
					if err := cw.reloadCertificate(); err != nil {
						log.Error(err, "Failed to reload certificate")
					}
				}
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			log.Error(err, "Certificate watcher error")

		case <-ctx.Done():
			return watcher.Close()
		}
	}
}

// reloadCertificate reloads the certificate and calls the callback
func (cw *CertificateWatcher) reloadCertificate() error {
	// Add a small delay to ensure file write is complete
	time.Sleep(100 * time.Millisecond)

	cert, err := tls.LoadX509KeyPair(cw.certPath, cw.keyPath)
	if err != nil {
		return err
	}

	if cw.onReload != nil {
		cw.onReload(cert)
	}

	return nil
}
