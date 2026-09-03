package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
)

func main() {
	conexao, err := net.Dial("tcp", "localhost:8080")
	if err != nil {
		fmt.Println("nao consegui conectar:", err)
		return
	}
	defer conexao.Close()

	fmt.Println("conectado. digite uma mensagem e tecle enter (ctrl+c para sair)")

	teclado := bufio.NewReader(os.Stdin)
	servidor := bufio.NewReader(conexao)

	for {
		fmt.Print("> ")

		texto, err := teclado.ReadString('\n')
		if err != nil {
			return
		}

		_, err = fmt.Fprint(conexao, texto)
		if err != nil {
			fmt.Println("conexao perdida:", err)
			return
		}

		resposta, err := servidor.ReadString('\n')
		if err != nil {
			fmt.Println("servidor fechou a conexao")
			return
		}

		fmt.Print(resposta)
	}
}
