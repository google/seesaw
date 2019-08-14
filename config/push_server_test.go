package config

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math"
	"math/big"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	pb "github.com/google/seesaw/pb/config"
)

var (
	certDuration = 10 * time.Minute
	initConfig   = &pb.Cluster{
		SeesawVip: &pb.Host{Fqdn: proto.String("seesaw")},
	}
	vserverPort   = 39400
	rsaKeySize    = 2048
	emptyTLSCert  = tls.Certificate{}
	clientTimeout = 3 * time.Second
)

func newCA(name string) (*rsa.PrivateKey, *x509.Certificate, error) {
	key, err := rsa.GenerateKey(rand.Reader, rsaKeySize)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to create a private key for CA: %v", err)
	}
	now := time.Now()
	tmpl := x509.Certificate{
		SerialNumber: new(big.Int).SetInt64(0),
		Subject: pkix.Name{
			CommonName: name,
		},
		NotBefore:             now.UTC(),
		NotAfter:              now.Add(certDuration).UTC(),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	certDERBytes, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, key.Public(), key)
	if err != nil {
		return nil, nil, err
	}
	cert, err := x509.ParseCertificate(certDERBytes)
	if err != nil {
		return nil, nil, err
	}
	return key, cert, nil
}

func encodePrivateKeyPEM(key *rsa.PrivateKey) []byte {
	block := pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}
	return pem.EncodeToMemory(&block)
}

func encodeCertPEM(cert *x509.Certificate) []byte {
	block := pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.Raw,
	}
	return pem.EncodeToMemory(&block)
}

func newSignedCert(name string, caKey crypto.Signer, caCert *x509.Certificate, dns string, ips []string, usage x509.ExtKeyUsage) (tls.Certificate, error) {
	key, err := rsa.GenerateKey(rand.Reader, rsaKeySize)
	if err != nil {
		return emptyTLSCert, fmt.Errorf("unable to create a private key for server cert: %v", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).SetInt64(math.MaxInt64))
	if err != nil {
		return emptyTLSCert, err
	}
	certTmpl := x509.Certificate{
		Subject: pkix.Name{
			CommonName:   name,
			Organization: []string{"google"},
		},
		SerialNumber: serial,
		NotBefore:    caCert.NotBefore,
		NotAfter:     time.Now().Add(certDuration).UTC(),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
	}
	if len(dns) > 0 {
		certTmpl.DNSNames = []string{dns}
	}
	for _, ip := range ips {
		certTmpl.IPAddresses = append(certTmpl.IPAddresses, net.ParseIP(ip))
	}
	certDERBytes, err := x509.CreateCertificate(rand.Reader, &certTmpl, caCert, key.Public(), caKey)
	if err != nil {
		return emptyTLSCert, err
	}
	cert, err := x509.ParseCertificate(certDERBytes)
	if err != nil {
		return emptyTLSCert, err
	}
	tlsCert, err := tls.X509KeyPair(encodeCertPEM(cert), encodePrivateKeyPEM(key))
	if err != nil {
		return emptyTLSCert, err
	}
	return tlsCert, err
}

func setupPushServerClient() (*PushServer, *tls.Config, error) {
	caKey, caCert, err := newCA("seesaw-ca")
	if err != nil {
		return nil, nil, fmt.Errorf("newCA failed: %s", err)
	}
	rootCACerts := x509.NewCertPool()
	rootCACerts.AddCert(caCert)

	serverCert, err := newSignedCert("config-server-cert", caKey, caCert, "", []string{"127.0.0.1"}, x509.ExtKeyUsageServerAuth)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create server cert: %v", err)
	}

	clientCert, err := newSignedCert("vserver-put-client", caKey, caCert, "", nil, x509.ExtKeyUsageClientAuth)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create client cert: %v", err)
	}
	tlsConfig := &tls.Config{
		RootCAs:      rootCACerts,
		Certificates: []tls.Certificate{clientCert},
	}

	p := NewProvider(initConfig)
	s := NewPushServer(p, rootCACerts, serverCert, vserverPort)

	return s, tlsConfig, nil
}

func checkProvider(p *Provider, expected *pb.Cluster) error {
	raw, err := p.get()
	if err != nil {
		return fmt.Errorf("p.get() failed: %v", err)
	}

	got := &pb.Cluster{}
	if err := proto.Unmarshal(raw, got); err != nil {
		return fmt.Errorf("proto.Unmarshal(raw) failed: %s", err)
	}

	if !proto.Equal(got, expected) {
		return fmt.Errorf("got config %s but want %s", got, initConfig)
	}
	return nil
}

func checkConfigPush(tlsConfig *tls.Config) error {
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
		Timeout: clientTimeout,
	}

	succeed := false
	vs := &pb.Vserver{
		Name: proto.String("vs-1"),
		EntryAddress: &pb.Host{
			Fqdn: proto.String("test host"),
		},
		Rp: proto.String("n/a"), // not used
	}
	cluster := &pb.Cluster{
		SeesawVip: &pb.Host{Fqdn: proto.String("n/a")},
		Vserver:   []*pb.Vserver{vs},
	}
	raw, err := proto.Marshal(cluster)
	if err != nil {
		return fmt.Errorf("proto.Marshal(cluster) failed: %v", err)
	}
	url := fmt.Sprintf("https://127.0.0.1:%d/vservers", vserverPort)
	req, err := http.NewRequest("PUT", url, bytes.NewBuffer(raw))
	if err != nil {
		return fmt.Errorf("http.NewRequest failed: %v", err)
	}
	req.Header.Add(contentType, pbContentType)
	var resp *http.Response
	// Need retry a bit until the server is up
	for i := 0; i < 5; i = i + 1 {
		resp, err = client.Do(req)
		if err != nil {
			time.Sleep(1 * time.Second)
			continue
		}
		succeed = true
		break
	}
	if !succeed {
		return fmt.Errorf("client.Do(%s) failed: %v", url, err)
	}
	defer resp.Body.Close()

	if got, want := resp.StatusCode, http.StatusOK; got != want {
		return fmt.Errorf("bad http status got %d but want %d", got, want)
	}
	return nil
}

func TestConfigPush(t *testing.T) {
	s, clientConfig, err := setupPushServerClient()
	if err != nil {
		t.Fatalf("setupPushServerClient failed: %v", err)
	}

	go s.Run()
	defer s.Shutdown()

	if err := checkConfigPush(clientConfig); err != nil {
		t.Fatal(err)
	}

}

func TestConfigPushClientCert(t *testing.T) {
	s, clientConfig, err := setupPushServerClient()
	if err != nil {
		t.Fatalf("setupPushServerClient failed: %v", err)
	}

	go s.Run()
	defer s.Shutdown()

	// firstly just to make sure the /vservers is up
	if err := checkConfigPush(clientConfig); err != nil {
		t.Fatal(err)
	}

	// remove client certs to verify that server is doing client authentication.
	clientConfig.Certificates = nil
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: clientConfig,
		},
		Timeout: clientTimeout,
	}

	vs := &pb.Vserver{
		Name: proto.String("vs-1"),
		EntryAddress: &pb.Host{
			Fqdn: proto.String("test host"),
		},
		Rp: proto.String("n/a"), // not used
	}
	cluster := &pb.Cluster{
		SeesawVip: &pb.Host{Fqdn: proto.String("n/a")},
		Vserver:   []*pb.Vserver{vs},
	}
	raw, err := proto.Marshal(cluster)
	if err != nil {
		t.Fatalf("proto.Marshal(cluster) failed: %v", err)
	}
	url := fmt.Sprintf("https://127.0.0.1:%d/vservers", vserverPort)
	req, err := http.NewRequest("PUT", url, bytes.NewBuffer(raw))
	if err != nil {
		t.Fatalf("http.NewRequest failed: %v", err)
	}
	req.Header.Add(contentType, pbContentType)
	if _, err = client.Do(req); err == nil {
		t.Fatalf("/vservers doesn't do client authentication")
	}
}
