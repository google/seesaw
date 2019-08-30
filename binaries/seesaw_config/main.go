package main

import (
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"io/ioutil"
	"path"

	log "github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	"github.com/google/seesaw/common/server"
	"github.com/google/seesaw/config"
	pb "github.com/google/seesaw/pb/config"
)

var (
	initConfigFile = flag.String("init-config", "/etc/seesaw/init-cluster.pb", "Path to the initial cluster config")
	configPort     = flag.Int("config_port", 10255, "port for serving /config. Only bound to localhost.")
	vserverPort    = flag.Int("vserver_port", 20255, "port for serving /vservers")
	caCertFile     = flag.String("ca-cert", "/etc/seesaw/cacert/cert.pem", "the file path to CA certs file")
	serverCertDir  = flag.String("server-cert-dir", "/etc/seesaw/cert/", "The directory where server certs and key are stored. Assuming file names are 'cert.pem' and 'key.pem'.")
)

func main() {
	flag.Parse()

	initConfig, err := ioutil.ReadFile(*initConfigFile)
	if err != nil {
		log.Exitf("failed to load %s", *initConfigFile)
	}
	cluster := &pb.Cluster{}
	if err := proto.UnmarshalText(string(initConfig), cluster); err != nil {
		log.Exitf("invalid init config: %s", err)
	}

	caCerts, err := loadCACerts(*caCertFile)
	if err != nil {
		log.Exitf("failed ot load ca certs: %s", err)
	}
	serverCerts, err := loadCerts(path.Join(*serverCertDir, "cert.pem"), path.Join(*serverCertDir, "key.pem"))
	if err != nil {
		log.Exitf("failed ot load client cert: %s", err)
	}
	p := config.NewProvider(cluster)
	get := config.NewGetServer(p, serverCerts, *configPort)
	push := config.NewPushServer(p, caCerts, serverCerts, *vserverPort)

	server.ShutdownHandler(get)
	server.ShutdownHandler(push)

	go get.Run()
	push.Run()
}

func loadCACerts(caCertFile string) (*x509.CertPool, error) {
	caCerts := x509.NewCertPool()
	data, err := ioutil.ReadFile(caCertFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA cert file (%s): %s", caCertFile, err)
	}
	if ok := caCerts.AppendCertsFromPEM(data); !ok {
		return nil, fmt.Errorf("invalid CA certs in file %s", caCertFile)
	}
	return caCerts, nil
}

func loadCerts(certFile, keyFile string) (tls.Certificate, error) {
	certs, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to load X.509 key pair: %s", err)
	}
	return certs, nil
}
