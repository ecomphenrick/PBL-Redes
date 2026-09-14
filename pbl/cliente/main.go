package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"

	"vaijunto/protocolo"
)

func main() {
	conexao, err := net.Dial("tcp", "localhost:8080")
	if err != nil {
		fmt.Println("nao consegui conectar:", err)
		return
	}
	defer conexao.Close()

	fmt.Println("conectado. digite uma acao (tente: ping) ou ctrl+c para sair")

	teclado := bufio.NewReader(os.Stdin)
	servidor := bufio.NewReader(conexao)

	for {
		fmt.Print("acao> ")

		texto, err := teclado.ReadString('\n')
		if err != nil {
			return
		}

		// TrimSpace tira o \r\n do Enter. Sem isso a acao viraria "ping\r\n"
		// e nunca casaria com o case "ping" do servidor.
		acao := strings.TrimSpace(texto)
		if acao == "" {
			continue
		}

		pedido := protocolo.Pedido{Acao: acao}

		if err := protocolo.EnviarPedido(conexao, pedido); err != nil {
			fmt.Println("conexao perdida:", err)
			return
		}

		resposta, err := protocolo.LerResposta(servidor)
		if err != nil {
			fmt.Println("servidor fechou a conexao")
			return
		}

		if resposta.OK {
			fmt.Println("  ok:", resposta.Mensagem)
		} else {
			fmt.Println("  erro:", resposta.Erro)
		}
	}
}
