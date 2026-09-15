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

// intervaloPadrao e de quanto em quanto tempo o servidor procura reservas
// vencidas. Pode ser encurtado por variavel de ambiente na hora de demonstrar.
const intervaloPadrao = 30 * time.Second

// sessao guarda quem esta logado NAQUELA conexao.
//
// Repare que ela NAO precisa de mutex: cada goroutine de atender() cria a sua
// propria sessao como variavel local, entao ninguem compartilha nada. So o
// Banco, que e o mesmo para todos, precisa de protecao.
type sessao struct {
	usuario dados.Usuario
}

func main() {
	varredura := configurarTempos()
	banco := dados.Carregar(arquivoDeDados())

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

	fmt.Println("servidor ouvindo em :8080")

	for {
		conexao, err := ouvinte.Accept()
		if err != nil {
			fmt.Println("erro ao aceitar conexao:", err)
			continue
		}

		fmt.Println("cliente conectado:", conexao.RemoteAddr())
		go atender(conexao, banco)
	}
}

// expirarPeriodicamente roda para sempre, numa goroutine propria.
//
// Ela tambem mexe no Banco, entao disputa o mesmo mutex que as goroutines dos
// clientes -- e e justamente por isso que nao da problema.
func expirarPeriodicamente(banco *dados.Banco, intervalo time.Duration) {
	relogio := time.NewTicker(intervalo)
	defer relogio.Stop()

	for range relogio.C {
		if n := banco.ExpirarVencidas(); n > 0 {
			fmt.Printf("expirei %d reserva(s) nao paga(s)\n", n)
		}
	}
}

// arquivoDeDados diz onde o banco JSON fica gravado.
// No Docker apontamos para um volume, para os dados sobreviverem ao container.
func arquivoDeDados() string {
	if v := os.Getenv("VAIJUNTO_DADOS"); v != "" {
		return v
	}
	return "vaijunto.json"
}

// configurarTempos le as variaveis de ambiente que encurtam os prazos.
// Para demonstrar a expiracao sem esperar 10 minutos:
//
//	VAIJUNTO_RESERVA=20s VAIJUNTO_VARREDURA=5s go run ./servidor
func configurarTempos() time.Duration {
	if v := os.Getenv("VAIJUNTO_RESERVA"); v != "" {
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
	defer conexao.Close()

	leitor := bufio.NewReader(conexao)

	// A sessao nasce e morre junto com esta conexao.
	var s sessao

	for {
		pedido, err := protocolo.LerPedido(leitor)

		// A linha chegou inteira, mas nao e JSON valido: o problema e da
		// mensagem, nao da conexao. Avisa o cliente e continua atendendo.
		if errors.Is(err, protocolo.ErrMensagemInvalida) {
			protocolo.EnviarResposta(conexao, protocolo.Resposta{OK: false, Erro: err.Error()})
			continue
		}

		// Qualquer OUTRO erro e da conexao: o cliente fechou (EOF), caiu de
		// forma abrupta (connection reset) ou a rede sumiu. Em todos os casos
		// nao ha mais ninguem do outro lado, entao a goroutine precisa acabar.
		//
		// Uma versao anterior so encerrava no EOF e tratava o resto como JSON
		// ruim: numa queda abrupta, voltava a ler, recebia o mesmo erro na hora
		// e girava para sempre a 100% de um nucleo.
		if err != nil {
			if errors.Is(err, io.EOF) {
				fmt.Println("cliente desconectou:", conexao.RemoteAddr())
			} else {
				fmt.Printf("conexao com %v caiu: %v\n", conexao.RemoteAddr(), err)
			}
			return
		}

		fmt.Printf("recebido de %v: acao=%q\n", conexao.RemoteAddr(), pedido.Acao)

		resposta := executar(pedido, &s, banco)

		if err := protocolo.EnviarResposta(conexao, resposta); err != nil {
			fmt.Println("erro ao responder:", err)
			return
		}
	}
}

// executar decide o que fazer com cada acao. Este switch e o roteador do
// servidor: e o indice do sistema inteiro.
//
// A sessao vem como PONTEIRO (*sessao) porque o case "login" precisa
// MODIFICAR quem esta logado. Se viesse por valor, alteraria uma copia.
func executar(p protocolo.Pedido, s *sessao, banco *dados.Banco) protocolo.Resposta {
	// Barreira de autenticacao: vale para toda acao nova que criarmos daqui
	// em diante, sem precisar repetir a checagem em cada case.
	if exigeLogin(p.Acao) && s.usuario.Login == "" {
		return erro("faca login primeiro")
	}

	switch p.Acao {
	case "ping":
		return protocolo.Resposta{OK: true, Mensagem: "pong"}

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

// exigeLogin diz quais acoes precisam de usuario autenticado.
// So "ping", "login" e "registrar" ficam de fora -- quem esta criando conta
// ainda nao tem como estar logado. Todo o resto exige.
func exigeLogin(acao string) bool {
	return acao != "ping" && acao != "login" && acao != "registrar"
}
