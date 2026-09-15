package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"vaijunto/protocolo"
)

// cliente junta as tres coisas que toda tela precisa: por onde falar com o
// servidor, por onde ouvir a resposta, e por onde ler o teclado.
// Sem isso, cada funcao de menu teria que receber os tres como argumento.
type cliente struct {
	conexao  net.Conn
	servidor *bufio.Reader
	teclado  *bufio.Reader
}

func main() {
	conexao, err := net.Dial("tcp", endereco())
	if err != nil {
		fmt.Println("nao consegui conectar:", err)
		return
	}
	defer conexao.Close()

	c := &cliente{
		conexao:  conexao,
		servidor: bufio.NewReader(conexao),
		teclado:  bufio.NewReader(os.Stdin),
	}

	tipo, ok := c.entrada()
	if !ok {
		return
	}

	// O tipo do usuario decide qual menu aparece.
	if tipo == "motorista" {
		c.menuMotorista()
	} else {
		c.menuPassageiro()
	}
}

// endereco permite apontar para outro host, o que sera necessario no Docker:
//
//	VAIJUNTO_SERVIDOR=servidor:8080 go run ./cliente
func endereco() string {
	if v := os.Getenv("VAIJUNTO_SERVIDOR"); v != "" {
		return v
	}
	return "localhost:8080"
}

// ---------------------------------------------------------------------------
// Login
// ---------------------------------------------------------------------------

// entrada e a primeira tela: entrar com uma conta existente ou criar uma nova.
// Devolve o tipo do usuario, que decide qual menu aparece depois.
func (c *cliente) entrada() (string, bool) {
	fmt.Println("=== Vaijunto ===")

	for {
		fmt.Println("\n1) entrar   2) criar conta   0) sair")

		switch c.opcao() {
		case "1":
			if tipo, ok := c.entrar(); ok {
				return tipo, true
			}
		case "2":
			if tipo, ok := c.criarConta(); ok {
				return tipo, true
			}
		case "0", "":
			return "", false
		default:
			fmt.Println("  opcao invalida")
		}
	}
}

// entrar pede usuario e senha de uma conta que ja existe.
func (c *cliente) entrar() (string, bool) {
	usuario, ok := c.ler("usuario: ")
	if !ok {
		return "", false
	}

	senha, ok := c.ler("senha: ")
	if !ok {
		return "", false
	}

	return c.autenticar(usuario, senha)
}

// criarConta registra um usuario novo e ja entra com ele.
func (c *cliente) criarConta() (string, bool) {
	usuario, ok := c.ler("novo usuario: ")
	if !ok {
		return "", false
	}

	senha, ok := c.ler("senha: ")
	if !ok {
		return "", false
	}

	fmt.Println("  1) motorista (oferece caronas)")
	fmt.Println("  2) passageiro (procura caronas)")

	escolha, ok := c.ler("tipo: ")
	if !ok {
		return "", false
	}

	var tipo string
	switch escolha {
	case "1":
		tipo = "motorista"
	case "2":
		tipo = "passageiro"
	default:
		fmt.Println("  escolha 1 ou 2")
		return "", false
	}

	r, err := c.pedir(protocolo.Pedido{
		Acao:    "registrar",
		Usuario: usuario,
		Senha:   senha,
		Tipo:    tipo,
	})
	if err != nil {
		fmt.Println("conexao perdida:", err)
		return "", false
	}

	if !r.OK {
		fmt.Println("  erro:", r.Erro)
		return "", false
	}

	fmt.Println(" ", r.Mensagem)

	// Conta criada: entra direto, sem obrigar a digitar tudo de novo.
	return c.autenticar(usuario, senha)
}

// autenticar manda o login e devolve o tipo do usuario.
func (c *cliente) autenticar(usuario, senha string) (string, bool) {
	r, err := c.pedir(protocolo.Pedido{Acao: "login", Usuario: usuario, Senha: senha})
	if err != nil {
		fmt.Println("conexao perdida:", err)
		return "", false
	}

	if !r.OK {
		fmt.Println("  erro:", r.Erro)
		return "", false
	}

	fmt.Println(" ", r.Mensagem)
	return r.Tipo, true
}

// ---------------------------------------------------------------------------
// Menu do motorista
// ---------------------------------------------------------------------------

func (c *cliente) menuMotorista() {
	for {
		fmt.Println("\n1) cadastrar carona   2) minhas caronas   3) passageiros   4) cancelar carona   0) sair")

		switch c.opcao() {
		case "1":
			c.cadastrarCarona()
		case "2":
			c.listar(protocolo.Pedido{Acao: "minhas_caronas"}, "voce ainda nao tem caronas")
		case "3":
			c.passageiros()
		case "4":
			c.cancelarCarona()
		case "0", "":
			return
		default:
			fmt.Println("  opcao invalida")
		}
	}
}

func (c *cliente) cadastrarCarona() {
	texto, ok := c.ler("rota (cidades separadas por virgula): ")
	if !ok {
		return
	}

	// "Feira, Salvador, Ilheus" vira ["Feira" "Salvador" "Ilheus"].
	var rota []string
	for _, cidade := range strings.Split(texto, ",") {
		cidade = strings.TrimSpace(cidade)
		if cidade != "" {
			rota = append(rota, cidade)
		}
	}

	data, ok := c.ler("data (2026-09-14): ")
	if !ok {
		return
	}

	assentos, ok := c.lerNumero("assentos: ")
	if !ok {
		return
	}

	preco, ok := c.lerNumero("preco por trecho: ")
	if !ok {
		return
	}

	c.mostrar(c.pedir(protocolo.Pedido{
		Acao:     "cadastrar",
		Rota:     rota,
		Data:     data,
		Assentos: assentos,
		Preco:    preco,
	}))
}

// passageiros mostra quem esta em cada trecho de uma carona.
func (c *cliente) passageiros() {
	id, ok := c.lerNumero("numero da carona: ")
	if !ok {
		return
	}

	c.listar(protocolo.Pedido{Acao: "passageiros", CaronaID: id}, "carona sem dados")
}

// cancelarCarona pede confirmacao antes, porque desfaz as reservas de todos
// os passageiros daquela carona.
func (c *cliente) cancelarCarona() {
	id, ok := c.lerNumero("numero da carona: ")
	if !ok {
		return
	}

	resposta, ok := c.ler(fmt.Sprintf("cancelar a carona %d? as reservas dos passageiros serao desfeitas (s/n): ", id))
	if !ok || strings.ToLower(resposta) != "s" {
		fmt.Println("  nada foi cancelado")
		return
	}

	c.mostrar(c.pedir(protocolo.Pedido{Acao: "cancelar_carona", CaronaID: id}))
}

// ---------------------------------------------------------------------------
// Menu do passageiro
// ---------------------------------------------------------------------------

func (c *cliente) menuPassageiro() {
	for {
		fmt.Println("\n1) buscar e reservar   2) minhas reservas   3) pagar   4) cancelar reserva   0) sair")

		switch c.opcao() {
		case "1":
			c.buscarEReservar()
		case "2":
			c.listar(protocolo.Pedido{Acao: "minhas_reservas"}, "voce ainda nao tem reservas")
		case "3":
			c.pagar()
		case "4":
			c.cancelarReserva()
		case "0", "":
			return
		default:
			fmt.Println("  opcao invalida")
		}
	}
}

func (c *cliente) buscarEReservar() {
	origem, ok := c.ler("origem: ")
	if !ok {
		return
	}

	destino, ok := c.ler("destino: ")
	if !ok {
		return
	}

	data, ok := c.ler("data (2026-09-14): ")
	if !ok {
		return
	}

	r, err := c.pedir(protocolo.Pedido{
		Acao:    "buscar",
		Origem:  origem,
		Destino: destino,
		Data:    data,
	})
	if err != nil {
		fmt.Println("conexao perdida:", err)
		return
	}

	if !r.OK {
		fmt.Println("  erro:", r.Erro)
		return
	}

	if len(r.Opcoes) == 0 {
		fmt.Println("  nenhuma carona encontrada")
		return
	}

	// O servidor ja mandou o Resumo pronto: aqui so numeramos e imprimimos.
	for i, o := range r.Opcoes {
		fmt.Printf("  %d) %s | R$%d\n", i+1, o.Resumo, o.Preco)
	}

	escolha, ok := c.lerNumero("reservar qual? (0 cancela): ")
	if !ok || escolha == 0 {
		return
	}

	if escolha < 1 || escolha > len(r.Opcoes) {
		fmt.Println("  opcao invalida")
		return
	}

	c.mostrar(c.pedir(protocolo.Pedido{
		Acao:  "reservar",
		Itens: r.Opcoes[escolha-1].Itens,
	}))
}

func (c *cliente) pagar() {
	id, ok := c.lerNumero("numero da reserva: ")
	if !ok {
		return
	}

	c.mostrar(c.pedir(protocolo.Pedido{Acao: "pagar", ReservaID: id}))
}

func (c *cliente) cancelarReserva() {
	id, ok := c.lerNumero("numero da reserva: ")
	if !ok {
		return
	}

	c.mostrar(c.pedir(protocolo.Pedido{Acao: "cancelar_reserva", ReservaID: id}))
}

// ---------------------------------------------------------------------------
// Peças reaproveitadas
// ---------------------------------------------------------------------------

// pedir envia um Pedido e espera a Resposta. Toda conversa com o servidor
// passa por aqui.
func (c *cliente) pedir(p protocolo.Pedido) (protocolo.Resposta, error) {
	if err := protocolo.EnviarPedido(c.conexao, p); err != nil {
		return protocolo.Resposta{}, err
	}
	return protocolo.LerResposta(c.servidor)
}

// listar faz um pedido que devolve Linhas e imprime uma por uma.
func (c *cliente) listar(p protocolo.Pedido, seVazio string) {
	r, err := c.pedir(p)
	if err != nil {
		fmt.Println("conexao perdida:", err)
		return
	}

	if !r.OK {
		fmt.Println("  erro:", r.Erro)
		return
	}

	if len(r.Linhas) == 0 {
		fmt.Println(" ", seVazio)
		return
	}

	for _, linha := range r.Linhas {
		fmt.Println("  " + linha)
	}
}

// mostrar imprime o resultado de um pedir(). Recebe os DOIS retornos de uma
// vez, o que deixa as chamadas curtas: c.mostrar(c.pedir(...)).
func (c *cliente) mostrar(r protocolo.Resposta, err error) {
	if err != nil {
		fmt.Println("conexao perdida:", err)
		return
	}

	if r.OK {
		fmt.Println(" ", r.Mensagem)
	} else {
		fmt.Println("  erro:", r.Erro)
	}
}

func (c *cliente) opcao() string {
	escolha, ok := c.ler("> ")
	if !ok {
		return "0"
	}
	return escolha
}

// ler mostra o rotulo e le uma linha do teclado. O segundo retorno e false
// quando a entrada acabou (ctrl+Z no Windows, ctrl+D no Linux).
func (c *cliente) ler(rotulo string) (string, bool) {
	fmt.Print(rotulo)

	texto, err := c.teclado.ReadString('\n')
	if err != nil {
		return "", false
	}

	// TrimSpace tira o \r\n do Enter. Sem isso "ping" viraria "ping\r\n" e
	// nunca casaria com o case do servidor.
	return strings.TrimSpace(texto), true
}

func (c *cliente) lerNumero(rotulo string) (int, bool) {
	texto, ok := c.ler(rotulo)
	if !ok {
		return 0, false
	}

	n, err := strconv.Atoi(texto)
	if err != nil {
		fmt.Println("  precisa ser um numero")
		return 0, false
	}

	return n, true
}
