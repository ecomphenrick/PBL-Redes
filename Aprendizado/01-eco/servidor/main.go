package main

import (
	"bufio"
	"fmt"
	"net"
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
		mensagem, err := leitor.ReadString('\n')
		if err != nil {
			fmt.Println("cliente desconectou:", conexao.RemoteAddr())
			return
		}

		fmt.Printf("recebido de %v: %s", conexao.RemoteAddr(), mensagem)
		fmt.Fprintf(conexao, "ECO: %s", mensagem)
	}
}
