package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"

	"vaijunto/protocolo"
)

func main() {
	ouvinte, err := net.Listen("tcp", ":8080")
	if err != nil {
		fmt.Println("erro ao abrir a porta:", err)
		return
	}
	defer ouvinte.Close()

	fmt.Println("servidor ouvindo em :8080")

	for {
		conexao, err := ouvinte.Accept()
		if err != nil {
			fmt.Println("erro ao aceitar conexao:", err)
			continue
		}

		fmt.Println("cliente conectado:", conexao.RemoteAddr())
		go atender(conexao)
	}
}

func atender(conexao net.Conn) {
	defer conexao.Close()

	leitor := bufio.NewReader(conexao)

	for {
		pedido, err := protocolo.LerPedido(leitor)
		if err != nil {
			// EOF significa que o cliente fechou a conexao: encerra a goroutine.
			if errors.Is(err, io.EOF) {
				fmt.Println("cliente desconectou:", conexao.RemoteAddr())
				return
			}
			// Qualquer outro erro e JSON malformado: avisa e continua ouvindo.
			protocolo.EnviarResposta(conexao, protocolo.Resposta{
				OK:   false,
				Erro: err.Error(),
			})
			continue
		}

		fmt.Printf("recebido de %v: acao=%q\n", conexao.RemoteAddr(), pedido.Acao)

		resposta := executar(pedido)

		if err := protocolo.EnviarResposta(conexao, resposta); err != nil {
			fmt.Println("erro ao responder:", err)
			return
		}
	}
}

// executar decide o que fazer com cada acao. Este switch e o roteador do
// servidor: cada fase seguinte adiciona um case aqui.
func executar(p protocolo.Pedido) protocolo.Resposta {
	switch p.Acao {
	case "ping":
		return protocolo.Resposta{OK: true, Mensagem: "pong"}

	default:
		return protocolo.Resposta{
			OK:   false,
			Erro: fmt.Sprintf("acao desconhecida: %q", p.Acao),
		}
	}
}
