package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/signal"

	"golang.org/x/crypto/ssh"
)

var (
	localAddrStr  = ""
	sshAddrStr    = ""
	remoteAddrStr = ""
	user          = ""
	password      = ""
	keyfile       = ""
)

func checkErr(err error) {
	if err != nil {
		log.Fatalln(err)
	}
}

func parseFlags() {
	flag.StringVar(&localAddrStr, "local", "127.0.0.1:10080", "Local address and port to map; default: 127.0.0.1:10080")
	flag.StringVar(&sshAddrStr, "ssh", "127.0.0.1:22", "SSH host ; default: 127.0.0.1:22")
	flag.StringVar(&remoteAddrStr, "remote", "", "Remote address and port to map; e.g. 127.0.0.1:80 (a sample of web server that runs on the same machine as SSH host)")
	flag.StringVar(&user, "user", "", "SSH user to login")
	flag.StringVar(&password, "pwd", "", "SSH user password to login (if the system uses password authentication)")
	flag.StringVar(&keyfile, "key", "", "SSH user private key file path (e.g. /home/user/.ssh/id_rsa)")
	flag.Parse()
}

func forward(orgConn, sshFwdConn net.Conn, errCh chan error) {
	go func() {
		_, err := io.Copy(orgConn, sshFwdConn)
		if err != nil {
			_e := fmt.Errorf("io.Copy failed (remote -> local): %v\n", err)
			errCh <- _e
			return
		}
		log.Println("Data sent to local host: " + orgConn.LocalAddr().String())
	}()

	_, err := io.Copy(sshFwdConn, orgConn)
	if err != nil {
		_e := fmt.Errorf("io.Copy failed (local -> remote): %v\n", err)
		errCh <- _e
		return
	}
	log.Println("Data sent to remote host: " + sshFwdConn.RemoteAddr().String())
}

func main() {
	parseFlags()
	if remoteAddrStr == "" {
		log.Fatalln("Remote Address is missing...")
	}
	log.Printf("Listening: %v; SSH Host: %v; Forwarding: %v\n\n", localAddrStr, sshAddrStr, remoteAddrStr)

	var auth ssh.AuthMethod
	switch {
	case keyfile != "":
		b, err := ioutil.ReadFile(keyfile)
		checkErr(err)
		sign, err := ssh.ParsePrivateKey(b)
		checkErr(err)
		auth = ssh.PublicKeys(sign)
	case password != "":
		auth = ssh.Password(password)
	}

	sshConf := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			auth,
		},
	}

	sshAddr, err := net.ResolveTCPAddr("tcp", sshAddrStr)
	checkErr(err)

	sshConn, err := ssh.Dial("tcp", sshAddr.String(), sshConf)
	checkErr(err)
	defer sshConn.Close()
	log.Println("SSH established at: " + sshAddrStr)

	remoteAddr, err := net.ResolveTCPAddr("tcp", remoteAddrStr)
	checkErr(err)

	sshFwdConn, err := sshConn.DialTCP("tcp", nil, remoteAddr)
	checkErr(err)
	defer sshFwdConn.Close()
	log.Println("SSH tunnel established at: " + remoteAddrStr)

	localAddr, err := net.ResolveTCPAddr("tcp", localAddrStr)
	checkErr(err)

	listener, err := net.ListenTCP("tcp", localAddr)
	checkErr(err)

	sig := make(chan os.Signal, 0)
	signal.Notify(sig, os.Interrupt, os.Kill)
	errCh := make(chan error, 0)
	go func() {
		for {
			select {
			case s := <-sig:
				log.Println("Shutting down SSH tunnel...")
				sig <- s
				return
			case e := <-errCh:
				log.Println(e)
				sig <- os.Interrupt
				return
			default:
				conn, err := listener.Accept()
				checkErr(err)
				go forward(conn, sshFwdConn, errCh)
			}
		}
	}()
	<-sig
	close(sig)
}
