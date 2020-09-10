package main

import (
	"errors"
	"fmt"
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
	Upstream []string            `yaml:"upstream"`
	NXDomain []string            `yaml:"nxdomain"`
	Redirect map[string][]string `yaml:"redirect"`
	Const    map[string][]net.IP `yaml:"const"`
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

	resolveWithConst := func(req *dns.Msg) (*dns.Msg, bool, error) {
		// const判定
		question := req.Question[0]
		checkConst := question.Qtype == dns.TypeA || question.Qtype == dns.TypeAAAA
		if checkConst {
			for domain, ips := range config.Const {
				domain = domain + "."
				if question.Name == domain {
					msg := new(dns.Msg)
					msg.SetReply(req)
					msg.Authoritative = true
					answers := []dns.RR{}
					for _, ip := range ips {
						if ip.To4() != nil {
							if question.Qtype == dns.TypeA {
								a := new(dns.A)
								a.A = ip
								a.Hdr = dns.RR_Header{Name: domain, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 0}
								answers = append(answers, a)
							}
						} else {
							if question.Qtype == dns.TypeAAAA {
								aaaa := new(dns.AAAA)
								aaaa.AAAA = ip
								aaaa.Hdr = dns.RR_Header{Name: domain, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: 0}
								answers = append(answers, aaaa)
							}
						}
					}
					msg.Answer = answers
					return msg, true, nil
				}
			}
		}
		// constじゃなかったバージョン
		res, _, err := client.Exchange(req, config.Upstream[0])
		return res, false, err
	}

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
		// redirect判定
		isRedirect := false
		origName := question.Name
		replacedName := ""
		for dest, domains := range config.Redirect {
			for _, domain := range domains {
				if strings.HasSuffix(question.Name, domain+".") {
					log.Printf(colorstring.Color("[yellow][REDIR]\t%s\t%s\t%s"), writer.RemoteAddr().String(), question.String(), dest)
					question.Name = dest + "."
					replacedName = dest + "."
					isRedirect = true
				}
			}
		}
		res, isConst, err := resolveWithConst(req)
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) { // タイムアウトはだまっとく
				fmt.Printf("[NOT CRITICAL ERROR]\t%s\n", err.Error())
				return
			}
			panic(err)
		}
		if !isRedirect { // redirect の場合もうログ出力はしてあるのでもう書かなくてよい
			if isConst {
				log.Printf(colorstring.Color("[green][CONST]\t%s\t%s"), writer.RemoteAddr().String(), req.Question[0].String())
			} else {
				log.Printf("[PASS]\t%s\t%s", writer.RemoteAddr().String(), req.Question[0].String())
			}
		}
		for i, question := range res.Question {
			if question.Name == replacedName {
				res.Question[i].Name = origName
			}
		}
		if err != nil {
			log.Fatalln(err)
		}
		for i, answer := range res.Answer {
			// TODO: CNAME 除去したほうがいいかも
			if isRedirect && answer.Header().Name == replacedName {
				answer.Header().Name = origName
				res.Answer[i] = answer
			}
		}
		writer.WriteMsg(res)
	})
	if false {
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
	}

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
