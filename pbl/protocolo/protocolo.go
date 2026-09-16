package protocolo

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

var ErrMensagemInvalida = errors.New("mensagem invalida") //json invalido

type Item struct {
	CaronaID int `json:"carona_id"`
	De       int `json:"de"`
	Ate      int `json:"ate"`
}

// padronizando structs client-server
type Opcao struct {
	Itens  []Item `json:"itens"`
	Resumo string `json:"resumo"`
	Preco  int    `json:"preco"`
}

type Pedido struct {
	Acao    string `json:"acao"`
	Usuario string `json:"usuario,omitempty"`
	Senha   string `json:"senha,omitempty"`
	Tipo    string `json:"tipo,omitempty"` // motorista ou passageiro

	// cadastrar
	Rota     []string `json:"rota,omitempty"`
	Data     string   `json:"data,omitempty"`
	Assentos int      `json:"assentos,omitempty"`
	Preco    int      `json:"preco,omitempty"`

	// buscar
	Origem  string `json:"origem,omitempty"`
	Destino string `json:"destino,omitempty"`

	// reservar
	Itens []Item `json:"itens,omitempty"`

	// passageiros, cancelar_carona
	CaronaID int `json:"carona_id,omitempty"`

	// pagar, cancelar_reserva
	ReservaID int `json:"reserva_id,omitempty"`
}

// Resposta e o que o servidor devolve para o cliente.
type Resposta struct {
	OK       bool   `json:"ok"`
	Erro     string `json:"erro,omitempty"`
	Mensagem string `json:"mensagem,omitempty"`

	Tipo      string   `json:"tipo,omitempty"`       // login
	CaronaID  int      `json:"carona_id,omitempty"`  // cadastrar
	Opcoes    []Opcao  `json:"opcoes,omitempty"`     // buscar
	Linhas    []string `json:"linhas,omitempty"`     // listagens
	ReservaID int      `json:"reserva_id,omitempty"` // reservar
}

// recebe um leitor e devolve um pedido e um erro
func LerPedido(leitor *bufio.Reader) (Pedido, error) {
	linha, err := leitor.ReadString('\n') //le tudo ate achar um /n
	if err != nil {
		return Pedido{}, err //retorna o erro que ocorreu e um pedido vazio
	}

	var p Pedido                                              //cria um pedido vazio
	if err := json.Unmarshal([]byte(linha), &p); err != nil { //recebe o json, transforma em um pedido, unmarshal usa bytes.
		return Pedido{}, fmt.Errorf("%w: %v", ErrMensagemInvalida, err) //se o json nao estiver no formato correto.
	}
	return p, nil //p preenchido e erro = nulo
}

func LerResposta(leitor *bufio.Reader) (Resposta, error) {
	linha, err := leitor.ReadString('\n')
	if err != nil {
		return Resposta{}, err
	}

	var r Resposta
	if err := json.Unmarshal([]byte(linha), &r); err != nil {
		return Resposta{}, fmt.Errorf("%w: %v", ErrMensagemInvalida, err)
	}
	return r, nil
}

// EnviarPedido escreve o Pedido como uma linha de JSON.
func EnviarPedido(destino io.Writer, p Pedido) error {
	return enviar(destino, p) //chama enviar
}

// EnviarResposta escreve a Resposta como uma linha de JSON.
func EnviarResposta(destino io.Writer, r Resposta) error {
	return enviar(destino, r)
}

// caminho contrario de ler.
func enviar(destino io.Writer, valor any) error {
	bytes, err := json.Marshal(valor) //struck para json bytes
	if err != nil {
		return err
	}

	bytes = append(bytes, '\n')

	_, err = destino.Write(bytes)
	return err
}
