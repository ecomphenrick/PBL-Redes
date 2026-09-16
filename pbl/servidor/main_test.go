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

// servidorDeTeste sobe um ouvinte numa porta livre e atende UMA conexao.
// O canal devolvido e fechado quando o atender() daquela conexao termina.
func servidorDeTeste(t *testing.T) (endereco string, terminou chan struct{}) {
	t.Helper()

	// Porta 0: o sistema operacional escolhe uma porta livre sozinho.
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

// TestQuedaAbruptaEncerraAtendimento garante que o servidor aguenta um
// cliente que cai sem se despedir: processo morto, cabo puxado, wifi caiu.
//
// Nesses casos a conexao termina com RST, e a leitura devolve "connection
// reset" -- que NAO e io.EOF. Uma versao anterior do atender tratava qualquer
// erro diferente de EOF como JSON malformado e voltava a ler: a goroutine
// girava para sempre, consumindo 100% de um nucleo.
func TestQuedaAbruptaEncerraAtendimento(t *testing.T) {
	endereco, terminou := servidorDeTeste(t)

	cliente, err := net.Dial("tcp", endereco)
	if err != nil {
		t.Fatalf("nao consegui conectar: %v", err)
	}

	// SetLinger(0) faz o Close mandar RST em vez do encerramento educado.
	cliente.(*net.TCPConn).SetLinger(0)
	time.Sleep(100 * time.Millisecond) // garante que o atender ja esta lendo
	cliente.Close()

	select {
	case <-terminou:
		// certo: a goroutine percebeu a queda e encerrou
	case <-time.After(2 * time.Second):
		t.Fatal("o atendimento nao terminou depois da queda do cliente: goroutine presa em laco")
	}
}

// TestJSONInvalidoNaoDerrubaConexao e o outro lado da mesma moeda: uma linha
// malformada NAO pode encerrar a conexao. O servidor responde o erro e
// continua atendendo aquele cliente.
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

// TestClienteOciosoEDesconectado: cliente conectado que nao manda nada tem a
// conexao encerrada pelo servidor quando estoura o tempoOcioso.
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

	// Nao manda nada: so espera.
	select {
	case <-terminou:
		// certo: o servidor desistiu do cliente mudo
	case <-time.After(2 * time.Second):
		t.Fatal("o servidor nao encerrou a conexao ociosa")
	}
}
