package main

import (
	"bufio"
	"fmt"
	"net"
	"testing"
	"time"

	"vaijunto/dados"
	"vaijunto/protocolo"
)

func servidorDeTeste(t *testing.T) (endereco string, terminou chan struct{}) {
	t.Helper()

	ouvinte, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("nao consegui abrir porta: %v", err)
	}
	t.Cleanup(func() { ouvinte.Close() })

	terminou = make(chan struct{})

	go func() {
		conexao, err := ouvinte.Accept()
		if err != nil {
			return
		}
		atender(conexao, dados.NovoBanco())
		close(terminou)
	}()

	return ouvinte.Addr().String(), terminou
}

func TestQuedaAbruptaEncerraAtendimento(t *testing.T) {
	endereco, terminou := servidorDeTeste(t)

	cliente, err := net.Dial("tcp", endereco)
	if err != nil {
		t.Fatalf("nao consegui conectar: %v", err)
	}

	cliente.(*net.TCPConn).SetLinger(0)
	time.Sleep(100 * time.Millisecond)
	cliente.Close()

	select {
	case <-terminou:

	case <-time.After(2 * time.Second):
		t.Fatal("o atendimento nao terminou depois da queda do cliente: goroutine presa em laco")
	}
}

func TestJSONInvalidoNaoDerrubaConexao(t *testing.T) {
	endereco, _ := servidorDeTeste(t)

	cliente, err := net.Dial("tcp", endereco)
	if err != nil {
		t.Fatalf("nao consegui conectar: %v", err)
	}
	defer cliente.Close()

	leitor := bufio.NewReader(cliente)

	fmt.Fprint(cliente, "isso nao e json\n")

	r, err := protocolo.LerResposta(leitor)
	if err != nil {
		t.Fatalf("o servidor deveria responder o erro, mas: %v", err)
	}
	if r.OK {
		t.Error("JSON invalido deveria voltar com ok=false")
	}

	fmt.Fprint(cliente, `{"acao":"ping"}`+"\n")

	r, err = protocolo.LerResposta(leitor)
	if err != nil {
		t.Fatalf("a conexao deveria continuar viva depois do JSON invalido: %v", err)
	}
	if !r.OK || r.Mensagem != "pong" {
		t.Errorf("esperava pong, veio %+v", r)
	}
}

func TestClienteOciosoEDesconectado(t *testing.T) {
	original := tempoOcioso
	tempoOcioso = 200 * time.Millisecond
	defer func() { tempoOcioso = original }()

	endereco, terminou := servidorDeTeste(t)

	cliente, err := net.Dial("tcp", endereco)
	if err != nil {
		t.Fatalf("nao consegui conectar: %v", err)
	}
	defer cliente.Close()

	select {
	case <-terminou:

	case <-time.After(2 * time.Second):
		t.Fatal("o servidor nao encerrou a conexao ociosa")
	}
}
