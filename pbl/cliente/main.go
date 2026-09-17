package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"vaijunto/protocolo"
)

// Timeouts do cliente. Sem eles, um IP errado ou um servidor travado deixaria
// o programa parado sem mensagem nenhuma.
const (
	prazoConexao  = 5 * time.Second  // para conseguir conectar
	prazoResposta = 10 * time.Second // para o servidor responder cada pedido
)

type cliente struct {
	conexao  net.Conn //conexão tcp com o server
	servidor *bufio.Reader
	teclado  *bufio.Reader
}

func main() {
	conexao, err := net.DialTimeout("tcp", endereco(), prazoConexao) //abre conexão tcp com o server
	if err != nil {
		fmt.Println("nao consegui conectar:", err)
		return
	}
	defer conexao.Close()

	c := &cliente{
		conexao:  conexao,
		servidor: bufio.NewReader(conexao),
		teclado:  bufio.NewReader(os.Stdin),
	} //usa a struct

	// Ao sair do menu do motorista/passageiro, volta para o menu inicial.
	// So o 0 do menu inicial fecha o programa.
	for {
		tipo, ok := c.entrada() //chama o menu inicial.
		if !ok {
			return
		}

		if tipo == "motorista" {
			c.menuMotorista()
		} else {
			c.menuPassageiro()
		}

		c.sair()
	}
}

// sair avisa o servidor para esquecer o login desta conexao, assim a proxima
// pessoa a entrar nao herda a sessao anterior.
func (c *cliente) sair() {
	if _, err := c.pedir(protocolo.Pedido{Acao: "sair"}); err != nil {
		fmt.Println("conexao perdida:", err)
	}
}

func endereco() string {
	if v := os.Getenv("VAIJUNTO_SERVIDOR"); v != "" { //se existir variável de ambiente.
		return v
	}
	return "localhost:8080"
}

func (c *cliente) entrada() (string, bool) {
	for {
		menu("VAIJUNTO",
			"1) entrar",
			"2) criar conta",
			"0) sair")

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

func (c *cliente) criarConta() (string, bool) {
	fmt.Println("  (Enter vazio cancela)")

	usuario, ok := c.lerValido("novo usuario: ", func(string) error { return nil })
	if !ok {
		return "", false
	}

	senha, ok := c.lerValido("senha: ", func(string) error { return nil })
	if !ok {
		return "", false
	}

	fmt.Println("  1) motorista (oferece caronas)")
	fmt.Println("  2) passageiro (procura caronas)")

	escolha, ok := c.lerValido("tipo: ", func(t string) error {
		if t != "1" && t != "2" {
			return fmt.Errorf("escolha 1 ou 2")
		}
		return nil
	})
	if !ok {
		return "", false
	}

	tipo := "motorista"
	if escolha == "2" {
		tipo = "passageiro"
	}

	r, err := c.pedir(protocolo.Pedido{ //pedido com ação (json e envia pro server)
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
		fmt.Println("  erro:", r.Erro) //user ja existe
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

func (c *cliente) menuMotorista() {
	for {
		menu("MOTORISTA",
			"1) cadastrar carona",
			"2) minhas caronas",
			"3) cancelar carona",
			"0) voltar")

		switch c.opcao() {
		case "1":
			c.cadastrarCarona()
		case "2":
			c.minhasCaronas()
		case "3":
			c.cancelarCarona()
		case "0", "":
			return
		default:
			fmt.Println("  opcao invalida")
		}
	}
}

func (c *cliente) cadastrarCarona() {
	fmt.Println("  (Enter vazio cancela)")

	// Cada campo e conferido logo depois de digitado: se estiver errado,
	// pergunta de novo na hora, em vez de descobrir so no fim.
	texto, ok := c.lerValido("rota (cidades separadas por virgula): ", validarRota)
	if !ok {
		return
	}
	rota := separarRota(texto)

	data, ok := c.lerValido("data (2026-09-14): ", validarData)
	if !ok {
		return
	}

	assentos, ok := c.lerInteiro("assentos: ", 1)
	if !ok {
		return
	}

	preco, ok := c.lerInteiro("preco por trecho: ", 0)
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

// minhasCaronas lista as caronas do motorista e, se ele quiser, detalha uma
// delas: ocupacao e passageiros de cada trecho.
func (c *cliente) minhasCaronas() {
	if !c.listar(protocolo.Pedido{Acao: "minhas_caronas"}, "voce ainda nao tem caronas") {
		return
	}

	texto, ok := c.ler("\ndetalhar qual carona? (Enter volta): ")
	if !ok || texto == "" {
		return
	}

	id, err := strconv.Atoi(texto)
	if err != nil {
		fmt.Println("  precisa ser um numero")
		return
	}

	c.listar(protocolo.Pedido{Acao: "passageiros", CaronaID: id}, "carona sem dados")
}

func (c *cliente) cancelarCarona() {
	if !c.listar(protocolo.Pedido{Acao: "minhas_caronas"}, "voce ainda nao tem caronas") {
		return
	}

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

func (c *cliente) menuPassageiro() {
	for {
		menu("PASSAGEIRO",
			"1) buscar e reservar",
			"2) minhas reservas",
			"3) pagar reserva",
			"4) cancelar reserva",
			"0) voltar")

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
	fmt.Println("  (Enter vazio cancela)")

	origem, ok := c.lerValido("origem: ", validarCidade)
	if !ok {
		return
	}

	destino, ok := c.lerValido("destino: ", validarCidade)
	if !ok {
		return
	}

	data, ok := c.lerValido("data (2026-09-14): ", validarData)
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

	fmt.Println()
	for i, o := range r.Opcoes {
		item(fmt.Sprintf("%d) %s | total R$%d", i+1, o.Resumo, o.Preco))
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
	if !c.listar(protocolo.Pedido{Acao: "minhas_reservas"}, "voce ainda nao tem reservas") {
		return
	}

	id, ok := c.lerNumero("numero da reserva: ")
	if !ok {
		return
	}

	c.mostrar(c.pedir(protocolo.Pedido{Acao: "pagar", ReservaID: id}))
}

func (c *cliente) cancelarReserva() {
	if !c.listar(protocolo.Pedido{Acao: "minhas_reservas"}, "voce ainda nao tem reservas") {
		return
	}

	id, ok := c.lerNumero("numero da reserva: ")
	if !ok {
		return
	}

	c.mostrar(c.pedir(protocolo.Pedido{Acao: "cancelar_reserva", ReservaID: id}))
}

func (c *cliente) pedir(p protocolo.Pedido) (protocolo.Resposta, error) {
	// O prazo vale para enviar E receber. E renovado a cada pedido, entao o
	// tempo que a pessoa passa pensando no menu nao conta.
	c.conexao.SetDeadline(time.Now().Add(prazoResposta))

	if err := protocolo.EnviarPedido(c.conexao, p); err != nil {
		return protocolo.Resposta{}, err
	}
	return protocolo.LerResposta(c.servidor)
}

// listar imprime as linhas da resposta. Devolve true se havia algo para
// mostrar, assim quem chamou sabe se vale a pena pedir um numero depois.
func (c *cliente) listar(p protocolo.Pedido, seVazio string) bool {
	r, err := c.pedir(p)
	if err != nil {
		fmt.Println("conexao perdida:", err)
		return false
	}

	if !r.OK {
		fmt.Println("  erro:", r.Erro)
		return false
	}

	if len(r.Linhas) == 0 {
		fmt.Println(" ", seVazio)
		return false
	}

	fmt.Println()
	for _, linha := range r.Linhas {
		item(linha)
	}
	return true
}

// item quebra uma linha "carona 1 | 2026-09-20 | Feira -> Ilheus" em lista:
// o primeiro pedaco vira o titulo e o resto fica indentado embaixo.
func item(linha string) {
	// Linha que ja vem indentada e continuacao do item anterior (ex.: os
	// trechos em "passageiros"): so alinha com os detalhes.
	if strings.HasPrefix(linha, " ") {
		fmt.Println("    " + linha)
		return
	}

	partes := strings.Split(linha, " | ")

	fmt.Println("  " + partes[0])
	for _, p := range partes[1:] {
		fmt.Println("      " + p)
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

// menu imprime um titulo emoldurado e as opcoes uma embaixo da outra.
// "opcoes ...string" aceita quantas strings vierem, como uma lista.
func menu(titulo string, opcoes ...string) {
	linha := strings.Repeat("=", 32)

	fmt.Println()
	fmt.Println(linha)
	fmt.Println("  " + titulo)
	fmt.Println(linha)
	for _, o := range opcoes {
		fmt.Println("  " + o)
	}
	fmt.Println(linha)
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

// lerValido pergunta ate a resposta passar na validacao. Se estiver errada,
// mostra o motivo e pergunta de novo na hora. Enter vazio desiste.
func (c *cliente) lerValido(rotulo string, validar func(string) error) (string, bool) {
	for {
		texto, ok := c.ler(rotulo)
		if !ok || texto == "" {
			fmt.Println("  operacao cancelada")
			return "", false
		}

		if err := validar(texto); err != nil {
			fmt.Println("  " + err.Error())
			continue
		}

		return texto, true
	}
}

// lerInteiro pergunta ate vir um numero inteiro maior ou igual a minimo.
func (c *cliente) lerInteiro(rotulo string, minimo int) (int, bool) {
	texto, ok := c.lerValido(rotulo, func(t string) error {
		n, err := strconv.Atoi(t)
		if err != nil {
			return fmt.Errorf("precisa ser um numero inteiro")
		}
		if n < minimo {
			return fmt.Errorf("precisa ser pelo menos %d", minimo)
		}
		return nil
	})
	if !ok {
		return 0, false
	}

	n, _ := strconv.Atoi(texto) // ja foi validado acima
	return n, true
}

// separarRota transforma "Feira, Salvador, Ilheus" em ["Feira" "Salvador" "Ilheus"].
func separarRota(texto string) []string {
	var rota []string
	for _, cidade := range strings.Split(texto, ",") {
		cidade = strings.TrimSpace(cidade)
		if cidade != "" {
			rota = append(rota, cidade)
		}
	}
	return rota
}

// As validacoes abaixo repetem, no cliente, as regras que o servidor ja
// confere. O servidor continua validando: o cliente so avisa mais cedo.

func validarRota(texto string) error {
	if len(separarRota(texto)) < 2 {
		return fmt.Errorf("a rota precisa de pelo menos 2 cidades, separadas por virgula")
	}
	return nil
}

func validarData(texto string) error {
	if _, err := time.Parse("2006-01-02", texto); err != nil {
		return fmt.Errorf("data invalida: use o formato AAAA-MM-DD (ex.: 2026-09-20)")
	}
	return nil
}

func validarCidade(texto string) error {
	if strings.Contains(texto, ",") {
		return fmt.Errorf("informe uma cidade so")
	}
	return nil
}
