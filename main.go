package main

import (
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/goccy/go-yaml"
	"github.com/k0kubun/colorstring"
	"github.com/k0kubun/pp"
	"github.com/miekg/dns"
)

func ipFromAddr(addr net.Addr) net.IP {
	switch addr := addr.(type) {
	case *net.UDPAddr:
		return addr.IP
	case *net.TCPAddr:
		return addr.IP
	}
	panic("must be UDPAddr or TCPAddr")
}

type Config struct {
	Upstream []string                     `yaml:"upstream"`
	NXDomain []string                     `yaml:"nxdomain"`
	Redirect map[string][]string          `yaml:"redirect"`
	Dynamic  map[string]map[string]string `yaml:"direct"`
}

func main() {
	configBytes, err := ioutil.ReadFile("config.yml")
	if err != nil {
		panic(err)
	}
	config := Config{}
	if err := yaml.Unmarshal(configBytes, &config); err != nil {
		panic(err)
	}
	client := new(dns.Client)

	udpServer := dns.Server{Addr: "0.0.0.0:53", Net: "udp"}
	tcpServer := dns.Server{Addr: "0.0.0.0:53", Net: "tcp"}
	dns.HandleFunc(".", func(writer dns.ResponseWriter, req *dns.Msg) {
		if len(req.Question) != 1 {
			log.Fatalln(req.Question)
		}
		question := &req.Question[0]
		// TYPE65479 はよくわかんないので蹴っとく
		// iOS 14〜?
		if question.Qtype == 65479 {
			msg := new(dns.Msg)
			msg.SetReply(req)
			writer.WriteMsg(msg)
			return
		}
		// nxdomain判定
		for _, domain := range config.NXDomain {
			if strings.HasSuffix(question.Name, domain+".") {
				msg := new(dns.Msg)
				msg.SetReply(req)
				writer.WriteMsg(msg)
				log.Printf(colorstring.Color("[red][BLOCKED]\t%s\t%s"), writer.RemoteAddr().String(), question.String())
				return
			}
		}
		log.Printf("[PASS]\t%s\t%s\n", writer.RemoteAddr().String(), req.Question[0].String())
		res, _, err := client.Exchange(req, config.Upstream[0])
		if err != nil {
			log.Fatalln(err)
		}
		writer.WriteMsg(res)
	})
	dns.HandleFunc("internal.", func(writer dns.ResponseWriter, req *dns.Msg) {
		domain := req.Question[0].Name
		msg := new(dns.Msg)
		msg.SetReply(req)
		msg.Authoritative = true
		pp.Println(writer.RemoteAddr().String())
		ip := ipFromAddr(writer.RemoteAddr())
		answers := []dns.RR{}
		if ip.To4() != nil {
			a := new(dns.A)
			a.A = ip
			a.Hdr = dns.RR_Header{Name: domain, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 0}
			answers = append(answers, a)
		} else {
			aaaa := new(dns.AAAA)
			aaaa.AAAA = ip
			aaaa.Hdr = dns.RR_Header{Name: domain, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: 0}
			answers = append(answers, aaaa)
		}
		msg.Answer = answers
		writer.WriteMsg(msg)
	})

	go func() {
		err := udpServer.ListenAndServe()
		if err != nil {
			panic(err)
		}
	}()
	go func() {
		err := tcpServer.ListenAndServe()
		if err != nil {
			panic(err)
		}
	}()

	sig := make(chan os.Signal)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	for {
		select {
		case s := <-sig:
			log.Fatalf("Signal %d received, stopping...\n", s)
		}
	}
}
