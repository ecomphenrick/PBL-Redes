package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"vaijunto/dados"
	"vaijunto/protocolo"
)

const intervaloPadrao = 30 * time.Second //intervalo de varredura

// Timeouts de socket. Sao var (e nao const) para os testes poderem encurtar.
//
// tempoOcioso: se o cliente ficar esse tempo sem mandar NADA, o servidor
// encerra a conexao. Protege contra cliente que sumiu sem avisar (cabo
// puxado, maquina travada) e contra conexao aberta so para ocupar recurso.
//
// prazoEscrita: tempo maximo para conseguir entregar uma resposta. Se o
// cliente parou de ler, a goroutine nao fica presa para sempre no Write.
var (
	tempoOcioso  = 15 * time.Minute
	prazoEscrita = 10 * time.Second
)

type sessao struct {
	usuario dados.Usuario
} //guara o user que esta logado.

func main() {
	varredura := configurarTempos()           //configura os tempos de reserva e varredura.
	banco := dados.Carregar(arquivoDeDados()) //carrega o mesmo banco para as goroutines

	fmt.Printf("reserva expira em %s, varrendo a cada %s\n",
		dados.TempoDeReserva, varredura)

	// Goroutine de fundo: fica devolvendo assentos de reservas nao pagas.
	go expirarPeriodicamente(banco, varredura)

	ouvinte, err := net.Listen("tcp", ":8080")
	if err != nil {
		fmt.Println("erro ao abrir a porta:", err)
		return
	}
	defer ouvinte.Close()
	//abre server na porta 8080 e so fecha quando a main acabar
	fmt.Println("servidor ouvindo em :8080")

	for {
		conexao, err := ouvinte.Accept() //bloqueia ate cliente conectar e retorna conexao
		if err != nil {
			fmt.Println("erro ao aceitar conexao:", err)
			continue //se falhar o cliente, continua esperando o proximo
		}

		fmt.Println("cliente conectado:", conexao.RemoteAddr()) //se nao falhar continua para printar a porta conectada
		go atender(conexao, banco)                              //goroutine propria e volta ao inicio do for para esperar mais clientes.
	}
}

func expirarPeriodicamente(banco *dados.Banco, intervalo time.Duration) { //recebe o banco e o intervalo
	relogio := time.NewTicker(intervalo) //a cada intervalo coloca a hora em relogio.c
	defer relogio.Stop()                 //desliga o relógio quando a função terminar

	for range relogio.C { //loop infinito que só para quando a função terminar.
		if n := banco.ExpirarVencidas(); n > 0 { //conta quantas reservas foram expiradas.
			fmt.Printf("expirei %d reserva(s) nao paga(s)\n", n)
		}
	}
}

func arquivoDeDados() string {
	if v := os.Getenv("VAIJUNTO_DADOS"); v != "" {
		return v
	}
	return "vaijunto.json" //devolve o arquivo de dados, vaijunto.json ou algum outro específico.
}

func configurarTempos() time.Duration {
	if v := os.Getenv("VAIJUNTO_RESERVA"); v != "" { //para ler a variavel do docker 30s ou entao vale o tempo padrao de 10mi
		if d, err := time.ParseDuration(v); err == nil {
			dados.TempoDeReserva = d
		}
	}

	intervalo := intervaloPadrao
	if v := os.Getenv("VAIJUNTO_VARREDURA"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			intervalo = d
		}
	}

	return intervalo
}

func atender(conexao net.Conn, banco *dados.Banco) {
	defer conexao.Close() //so acaba quando a funcao atender acabar

	leitor := bufio.NewReader(conexao)

	var s sessao //sessao

	for {
		// Renova o prazo a cada pedido: o relogio conta o tempo PARADO desde a
		// ultima mensagem, nao o tempo total da conexao.
		conexao.SetReadDeadline(time.Now().Add(tempoOcioso))

		pedido, err := protocolo.LerPedido(leitor) //bloqueia ate chegar linha do cliente.

		// A linha chegou inteira, mas nao e JSON valido: o problema e da
		// mensagem, nao da conexao. Avisa o cliente e continua atendendo.
		if errors.Is(err, protocolo.ErrMensagemInvalida) {
			if responder(conexao, protocolo.Resposta{OK: false, Erro: err.Error()}) != nil {
				return
			}
			continue
		}

		// Estourou o tempoOcioso: o cliente ficou mudo tempo demais.
		if errors.Is(err, os.ErrDeadlineExceeded) {
			fmt.Printf("conexao com %v encerrada por inatividade\n", conexao.RemoteAddr())
			return
		}
		//se der problema com o server-client
		if err != nil {
			if errors.Is(err, io.EOF) {
				fmt.Println("cliente desconectou:", conexao.RemoteAddr())
			} else {
				fmt.Printf("conexao com %v caiu: %v\n", conexao.RemoteAddr(), err)
			}
			return
		}

		fmt.Printf("recebido de %v: acao=%q\n", conexao.RemoteAddr(), pedido.Acao) //acao valida

		resposta := executar(pedido, &s, banco)

		if err := responder(conexao, resposta); err != nil {
			fmt.Println("erro ao responder:", err)
			return
		} //transforma a resposta em json e envia pro cliente.
	}
}

// responder envia a resposta com prazo: se o cliente nao ler em prazoEscrita,
// o Write desiste com erro em vez de travar a goroutine.
func responder(conexao net.Conn, r protocolo.Resposta) error {
	conexao.SetWriteDeadline(time.Now().Add(prazoEscrita))
	return protocolo.EnviarResposta(conexao, r)
}

func executar(p protocolo.Pedido, s *sessao, banco *dados.Banco) protocolo.Resposta {
	// função roteador, olha a ação chama a função em dados e monta a resposta.
	if exigeLogin(p.Acao) && s.usuario.Login == "" {
		return erro("faca login primeiro")
	}
	//se nao tiver logado devolve erro.

	switch p.Acao {
	case "ping":
		return protocolo.Resposta{OK: true, Mensagem: "pong"} //padrao para teste

	case "registrar":
		if err := banco.CadastrarUsuario(p.Usuario, p.Senha, p.Tipo); err != nil {
			return erro(err.Error())
		}
		return protocolo.Resposta{
			OK:       true,
			Mensagem: fmt.Sprintf("conta criada para %s (%s)", p.Usuario, p.Tipo),
		}

	case "login":
		u, ok := banco.Autenticar(p.Usuario, p.Senha)
		if !ok {
			return erro("usuario ou senha invalidos")
		}

		s.usuario = u
		return protocolo.Resposta{
			OK:       true,
			Tipo:     u.Tipo,
			Mensagem: fmt.Sprintf("bem-vindo, %s", u.Login),
		}

	case "sair":
		s.usuario = dados.Usuario{}
		return protocolo.Resposta{OK: true, Mensagem: "sessao encerrada"}

	// ---- acoes do motorista ----

	case "cadastrar":
		if s.usuario.Tipo != "motorista" {
			return erro("so motorista cadastra carona")
		}

		id, err := banco.CadastrarCarona(s.usuario.Login, p.Rota, p.Data, p.Assentos, p.Preco)
		if err != nil {
			return erro(err.Error())
		}
		return protocolo.Resposta{
			OK:       true,
			CaronaID: id,
			Mensagem: fmt.Sprintf("carona %d cadastrada", id),
		}

	case "minhas_caronas":
		return protocolo.Resposta{OK: true, Linhas: banco.MinhasCaronas(s.usuario.Login)}

	case "passageiros":
		if s.usuario.Tipo != "motorista" {
			return erro("so motorista ve os passageiros de uma carona")
		}

		linhas, err := banco.PassageirosDaCarona(s.usuario.Login, p.CaronaID)
		if err != nil {
			return erro(err.Error())
		}
		return protocolo.Resposta{OK: true, Linhas: linhas}

	case "cancelar_carona":
		if s.usuario.Tipo != "motorista" {
			return erro("so motorista cancela carona")
		}

		afetadas, err := banco.CancelarCarona(s.usuario.Login, p.CaronaID)
		if err != nil {
			return erro(err.Error())
		}
		return protocolo.Resposta{
			OK: true,
			Mensagem: fmt.Sprintf("carona %d cancelada; %d reserva(s) desfeita(s) e assentos devolvidos",
				p.CaronaID, afetadas),
		}

	// ---- acoes do passageiro ----

	case "buscar":
		if !dados.DataValida(p.Data) {
			return erro(dados.ErrDataInvalida.Error())
		}

		opcoes := banco.Buscar(p.Origem, p.Destino, p.Data)
		return protocolo.Resposta{
			OK:       true,
			Opcoes:   opcoes,
			Mensagem: fmt.Sprintf("%d opcao(oes) encontrada(s)", len(opcoes)),
		}

	case "reservar":
		id, err := banco.Reservar(s.usuario.Login, p.Itens)
		if err != nil {
			return erro(err.Error())
		}
		return protocolo.Resposta{
			OK:        true,
			ReservaID: id,
			Mensagem: fmt.Sprintf("reserva %d criada; pague em ate %s ou os assentos voltam",
				id, dados.TempoDeReserva),
		}

	case "pagar":
		if err := banco.Pagar(s.usuario.Login, p.ReservaID); err != nil {
			return erro(err.Error())
		}
		return protocolo.Resposta{
			OK:       true,
			Mensagem: fmt.Sprintf("reserva %d paga; assentos confirmados", p.ReservaID),
		}

	case "cancelar_reserva":
		if err := banco.CancelarReserva(s.usuario.Login, p.ReservaID); err != nil {
			return erro(err.Error())
		}
		return protocolo.Resposta{
			OK:       true,
			Mensagem: fmt.Sprintf("reserva %d cancelada; assentos devolvidos", p.ReservaID),
		}

	case "minhas_reservas":
		return protocolo.Resposta{OK: true, Linhas: banco.MinhasReservas(s.usuario.Login)}

	default:
		return erro(fmt.Sprintf("acao desconhecida: %q", p.Acao))
	}
}

// erro monta uma Resposta de falha. Existe so para encurtar os cases.
func erro(mensagem string) protocolo.Resposta {
	return protocolo.Resposta{OK: false, Erro: mensagem}
}

// quais acoes precisam de login.
func exigeLogin(acao string) bool {
	return acao != "ping" && acao != "login" && acao != "registrar"
}
