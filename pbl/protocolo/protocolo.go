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

// Opcao e uma viagem possivel que o servidor oferece ao passageiro.
//
// O servidor ja monta o Resumo pronto para exibir. Assim o cliente nao precisa
// conhecer a estrutura interna das caronas: ele so imprime o texto e devolve
// os Itens da opcao escolhida.
type Opcao struct {
	Itens  []Item `json:"itens"`
	Resumo string `json:"resumo"`
	Preco  int    `json:"preco"`
}

// Pedido e o que o cliente manda para o servidor.
// Cada acao usa so os campos que interessam a ela; o resto vai vazio.
type Pedido struct {
	Acao    string `json:"acao"`
	Usuario string `json:"usuario,omitempty"`
	Senha   string `json:"senha,omitempty"`
	Tipo    string `json:"tipo,omitempty"` // registrar: motorista ou passageiro

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

// LerPedido le uma linha e transforma o JSON em Pedido.
//
// Dois tipos de erro podem voltar, e quem chama PRECISA distinguir:
//   - erro de leitura (EOF, conexao resetada): a conexao acabou;
//   - ErrMensagemInvalida: a linha veio, mas o JSON esta errado.
func LerPedido(leitor *bufio.Reader) (Pedido, error) {
	linha, err := leitor.ReadString('\n')
	if err != nil {
		return Pedido{}, err
	}

	var p Pedido
	if err := json.Unmarshal([]byte(linha), &p); err != nil {
		return Pedido{}, fmt.Errorf("%w: %v", ErrMensagemInvalida, err)
	}
	return p, nil
}

// LerResposta le uma linha e transforma o JSON em Resposta.
// Os erros seguem a mesma regra do LerPedido.
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
