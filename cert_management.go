package amp

import (
	"crypto/tls"
	"crypto/x509"
	"sync"
	"time"

	"go.uber.org/zap"
)

// From
// https://stackoverflow.com/a/40883377/265026

type KeypairReloader struct {
	logger   *zap.Logger
	certMu   sync.RWMutex
	cert     *tls.Certificate
	certPath string
	keyPath  string
}

func NewKeypairReloader(certPath, keyPath string, logger *zap.Logger) (*KeypairReloader, error) {
	kpr := &KeypairReloader{
		logger:   logger,
		certPath: certPath,
		keyPath:  keyPath,
	}

	logger.Info("NewKeypairReloader loading",
		zap.String("certPath", certPath),
		zap.String("keyPath", keyPath))

	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, err
	}

	kpr.cert = &cert
	go func() {
		err := kpr.certExpChecker()
		if err != nil {
			kpr.logger.Fatal("certExpChecker",
				zap.String("certPath", certPath),
				zap.String("keyPath", keyPath),
				zap.Error(err),
			)
		}
	}()

	//go func() {
	//	c := make(chan os.Signal, 1)
	//	signal.Notify(c, syscall.SIGHUP)
	//	for range c {
	//		logger.Info("Reloading TLS certificate and key",
	//			zap.String("cert", certPath), zap.String("key", keyPath))
	//		if err := result.maybeReload(); err != nil {
	//			logger.Error("Keeping old TLS certificate because the new one could not be loaded", zap.Error(err))
	//		}
	//	}
	//}()
	return kpr, nil
}

func (kpr *KeypairReloader) certExpChecker() error {
	for {
		// Parse the certificate data
		parsedCert, err := x509.ParseCertificate(kpr.cert.Certificate[0])
		if err != nil {
			return err
		}
		now := time.Now()
		expSecs := parsedCert.NotAfter.Unix() - now.Unix()

		kpr.logger.Info("Checking cert",
			zap.Int64("expiresInSec", expSecs))

		// if the cert expires in less than 10 minutes attempt a reload
		if expSecs < 600 {
			kpr.logger.Warn("TLS Certificate is about to expire or has expired, attempting reload",
				zap.Int64("expiresInSec", expSecs),
			)

			if err := kpr.maybeReload(); err != nil {
				kpr.logger.Error("Keeping old TLS certificate because the new one could not be loaded",
					zap.Error(err))
			}
		}

		// if expSecs < 600 then wait 10 seconds between checks
		waitTime := 10 * time.Second

		// if expSecs > 600 then wait 50% of remaining seconds
		if expSecs > 600 {
			waitTime = time.Duration(int64(float64(expSecs)*0.5)) * time.Second
		}

		kpr.logger.Info("Setting next certificate check",
			zap.Int64("expiresInSec", expSecs),
			zap.Duration("waitTime", waitTime))

		time.Sleep(waitTime)
	}
}

func (kpr *KeypairReloader) maybeReload() error {
	kpr.logger.Info("Attempting certificate reload",
		zap.String("certPath", kpr.certPath),
		zap.String("keyPath", kpr.keyPath),
	)
	newCert, err := tls.LoadX509KeyPair(kpr.certPath, kpr.keyPath)
	if err != nil {
		return err
	}
	kpr.certMu.Lock()
	defer kpr.certMu.Unlock()
	kpr.cert = &newCert
	return nil
}

func (kpr *KeypairReloader) GetCertificateFunc() func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	return func(clientHello *tls.ClientHelloInfo) (*tls.Certificate, error) {
		kpr.certMu.RLock()
		defer kpr.certMu.RUnlock()
		return kpr.cert, nil
	}
}
