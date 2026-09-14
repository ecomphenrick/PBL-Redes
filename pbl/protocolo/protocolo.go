// Package protocolo define as mensagens que cliente e servidor trocam.
//
// A regra do protocolo e simples: UMA LINHA DE TEXTO = UMA MENSAGEM, e o
// conteudo da linha e um JSON. E isso que resolve o problema de o TCP nao ter
// fronteira de mensagem: quem le sabe que a mensagem acabou quando acha o \n.
package protocolo

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

// Pedido e o que o cliente manda para o servidor.
// Por enquanto so tem os campos de login; os outros chegam nas proximas fases.
type Pedido struct {
	Acao    string `json:"acao"`
	Usuario string `json:"usuario,omitempty"`
	Senha   string `json:"senha,omitempty"`
}

// Resposta e o que o servidor devolve para o cliente.
type Resposta struct {
	OK       bool   `json:"ok"`
	Erro     string `json:"erro,omitempty"`
	Mensagem string `json:"mensagem,omitempty"`
}

// LerPedido le uma linha e transforma o JSON em Pedido.
func LerPedido(leitor *bufio.Reader) (Pedido, error) {
	linha, err := leitor.ReadString('\n')
	if err != nil {
		return Pedido{}, err
	}

	var p Pedido
	if err := json.Unmarshal([]byte(linha), &p); err != nil {
		return Pedido{}, fmt.Errorf("pedido invalido: %w", err)
	}
	return p, nil
}

// LerResposta le uma linha e transforma o JSON em Resposta.
func LerResposta(leitor *bufio.Reader) (Resposta, error) {
	linha, err := leitor.ReadString('\n')
	if err != nil {
		return Resposta{}, err
	}

	var r Resposta
	if err := json.Unmarshal([]byte(linha), &r); err != nil {
		return Resposta{}, fmt.Errorf("resposta invalida: %w", err)
	}
	return r, nil
}

// EnviarPedido escreve o Pedido como uma linha de JSON.
func EnviarPedido(destino io.Writer, p Pedido) error {
	return enviar(destino, p)
}

// EnviarResposta escreve a Resposta como uma linha de JSON.
func EnviarResposta(destino io.Writer, r Resposta) error {
	return enviar(destino, r)
}

// enviar converte qualquer valor em JSON e escreve com o \n no fim.
// E minuscula, entao so existe dentro deste pacote.
func enviar(destino io.Writer, valor any) error {
	bytes, err := json.Marshal(valor)
	if err != nil {
		return err
	}

	bytes = append(bytes, '\n')

	_, err = destino.Write(bytes)
	return err
}
