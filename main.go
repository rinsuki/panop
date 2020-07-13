package main

import (
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

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

func main() {
	udpServer := dns.Server{Addr: "0.0.0.0:53", Net: "udp"}
	tcpServer := dns.Server{Addr: "0.0.0.0:53", Net: "tcp"}
	dns.HandleFunc(".", func(writer dns.ResponseWriter, req *dns.Msg) {
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
