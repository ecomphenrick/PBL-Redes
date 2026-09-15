package dados

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"vaijunto/protocolo"
)

// trecho monta o item "carona X, da cidade De ate a cidade Ate".
func trecho(caronaID, de, ate int) []protocolo.Item {
	return []protocolo.Item{{CaronaID: caronaID, De: de, Ate: ate}}
}

// bancoComCarona devolve um banco novo com uma carona ja cadastrada.
func bancoComCarona(t *testing.T, rota []string, assentos int) (*Banco, int) {
	t.Helper()

	b := NovoBanco()
	id, err := b.CadastrarCarona("ana", rota, "2026-09-14", assentos, 30)
	if err != nil {
		t.Fatalf("nao consegui cadastrar a carona: %v", err)
	}

	return b, id
}

// ---------------------------------------------------------------------------
// O TESTE PRINCIPAL: concorrencia
// ---------------------------------------------------------------------------

// TestReservaConcorrente e o teste que prova o tratamento de concorrencia.
//
// Uma carona com UM assento e 50 goroutines tentando reservar ao mesmo tempo.
// So uma pode vencer. Sem o mutex em Reservar, varias leriam "1 livre" antes
// de qualquer uma escrever, e o sistema venderia o mesmo assento varias vezes.
//
// Rode com o detector de corrida:
//
//	go test -race ./...
//
// Para ver o teste falhar de proposito, comente o b.mu.Lock() de Reservar.
func TestReservaConcorrente(t *testing.T) {
	const tentativas = 200

	b, id := bancoComCarona(t, []string{"Feira", "Salvador"}, 1)

	// Canal com espaco para todos: cada goroutine que conseguir reservar
	// joga um aviso aqui. Usar canal evita precisar de outro mutex so para
	// contar os sucessos.
	sucessos := make(chan int, tentativas)

	// WaitGroup e o contador que espera todas as goroutines terminarem.
	var wg sync.WaitGroup

	// largada trava todas as goroutines no mesmo ponto. Sem isso, a primeira
	// a nascer ja teria reservado antes de a ultima existir, e nao haveria
	// disputa nenhuma para testar.
	largada := make(chan struct{})

	for i := 0; i < tentativas; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			<-largada // espera aqui ate o canal ser fechado

			if _, err := b.Reservar("bruno", trecho(id, 0, 1)); err == nil {
				sucessos <- 1
			}
		}()
	}

	close(largada) // solta as 50 de uma vez
	wg.Wait()      // espera todas terminarem
	close(sucessos)

	if n := len(sucessos); n != 1 {
		t.Errorf("esperava exatamente 1 reserva bem sucedida, deu %d", n)
	}

	c := b.acharCarona(id)
	if c.Ocupados[0] != 1 {
		t.Errorf("esperava o trecho com 1 assento ocupado, deu %d", c.Ocupados[0])
	}

	if n := len(b.Reservas); n != 1 {
		t.Errorf("esperava 1 reserva registrada, deu %d", n)
	}
}

// TestContagemDeAssentos e o teste que pega a falta do mutex de forma
// CONFIAVEL.
//
// O teste acima (1 assento, muitas goroutines) e a garantia de negocio, mas
// sozinho ele nao detecta bem o bug: quem largar primeiro ja ocupou o assento
// antes de as outras nascerem, e todas as demais falham "corretamente".
//
// Aqui e diferente: sao N assentos para N goroutines, entao TODAS entram na
// secao critica e TODAS escrevem. Sem o mutex, dois "Ocupados[t]++"
// simultaneos viram um so (o classico "lost update") e a contagem final fica
// MENOR que N.
func TestContagemDeAssentos(t *testing.T) {
	const pedidos = 2000

	b, id := bancoComCarona(t, []string{"Feira", "Salvador"}, pedidos)

	var wg sync.WaitGroup
	largada := make(chan struct{})

	for i := 0; i < pedidos; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-largada
			if _, err := b.Reservar("bruno", trecho(id, 0, 1)); err != nil {
				t.Errorf("reserva deveria funcionar, sobra assento: %v", err)
			}
		}()
	}

	close(largada)
	wg.Wait()

	if n := b.acharCarona(id).Ocupados[0]; n != pedidos {
		t.Errorf("perdi incrementos: Ocupados = %d, esperado %d", n, pedidos)
	}

	if n := len(b.Reservas); n != pedidos {
		t.Errorf("perdi reservas: %d registradas, esperado %d", n, pedidos)
	}
}

// TestExpiracaoConcorrente roda a expiracao ao mesmo tempo que as reservas.
// Sao duas goroutines mexendo no MESMO contador de assentos por caminhos
// diferentes -- exatamente o cenario do servidor de verdade.
func TestExpiracaoConcorrente(t *testing.T) {
	original := TempoDeReserva
	TempoDeReserva = time.Millisecond
	defer func() { TempoDeReserva = original }()

	b, id := bancoComCarona(t, []string{"Feira", "Salvador"}, 5)

	var wg sync.WaitGroup

	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.Reservar("bruno", trecho(id, 0, 1))
		}()
	}

	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.ExpirarVencidas()
		}()
	}

	wg.Wait()

	// O numero final depende do sorteio das goroutines, mas o estado tem que
	// ser COERENTE: nunca negativo, nunca acima do total de assentos.
	c := b.acharCarona(id)
	if c.Ocupados[0] < 0 || c.Ocupados[0] > c.Assentos {
		t.Errorf("contador de assentos incoerente: %d (total %d)", c.Ocupados[0], c.Assentos)
	}
}

// ---------------------------------------------------------------------------
// Atomicidade
// ---------------------------------------------------------------------------

// TestReservaAtomica confere o "pegou um trecho nao fica sem o outro".
// Se o segundo item falhar, o primeiro NAO pode ter sido aplicado.
func TestReservaAtomica(t *testing.T) {
	b, livre := bancoComCarona(t, []string{"Ilheus", "Porto Seguro"}, 2)

	cheia, err := b.CadastrarCarona("davi", []string{"Feira", "Salvador"}, "2026-09-14", 1, 30)
	if err != nil {
		t.Fatalf("nao consegui cadastrar: %v", err)
	}

	// Enche a segunda carona.
	if _, err := b.Reservar("carla", trecho(cheia, 0, 1)); err != nil {
		t.Fatalf("a primeira reserva deveria funcionar: %v", err)
	}

	// Agora pede as duas juntas. A carona livre vem PRIMEIRO de proposito:
	// se a implementacao aplicasse item a item, ela ja teria sido ocupada
	// quando a segunda falhasse.
	_, err = b.Reservar("bruno", []protocolo.Item{
		{CaronaID: livre, De: 0, Ate: 1},
		{CaronaID: cheia, De: 0, Ate: 1},
	})
	if err == nil {
		t.Fatal("a reserva deveria falhar: a segunda carona esta cheia")
	}

	if ocupados := b.acharCarona(livre).Ocupados[0]; ocupados != 0 {
		t.Errorf("a carona livre foi alterada apesar da falha: ocupados = %d", ocupados)
	}
}

// TestReservaMultiplaFunciona garante que o caso feliz de duas caronas
// tambem funciona -- senao o teste acima passaria com um Reservar que
// simplesmente recusa tudo.
func TestReservaMultiplaFunciona(t *testing.T) {
	b, primeira := bancoComCarona(t, []string{"Feira", "Salvador"}, 1)

	segunda, err := b.CadastrarCarona("davi", []string{"Salvador", "Ilheus"}, "2026-09-14", 1, 40)
	if err != nil {
		t.Fatalf("nao consegui cadastrar: %v", err)
	}

	if _, err := b.Reservar("bruno", []protocolo.Item{
		{CaronaID: primeira, De: 0, Ate: 1},
		{CaronaID: segunda, De: 0, Ate: 1},
	}); err != nil {
		t.Fatalf("a reserva das duas deveria funcionar: %v", err)
	}

	if n := b.acharCarona(primeira).Ocupados[0]; n != 1 {
		t.Errorf("primeira carona: esperava 1 ocupado, deu %d", n)
	}
	if n := b.acharCarona(segunda).Ocupados[0]; n != 1 {
		t.Errorf("segunda carona: esperava 1 ocupado, deu %d", n)
	}
}

// ---------------------------------------------------------------------------
// Disponibilidade por trecho
// ---------------------------------------------------------------------------

// TestDisponibilidadePorTrecho e o comportamento que diferencia o sistema:
// ocupar um trecho NAO ocupa a carona inteira.
func TestDisponibilidadePorTrecho(t *testing.T) {
	b, id := bancoComCarona(t, []string{"Feira", "Salvador", "Ilheus"}, 1)

	// Reserva so o primeiro trecho.
	if _, err := b.Reservar("bruno", trecho(id, 0, 1)); err != nil {
		t.Fatalf("deveria funcionar: %v", err)
	}

	casos := []struct {
		nome            string
		origem, destino string
		querOpcoes      int
	}{
		{"o trecho seguinte continua livre", "Salvador", "Ilheus", 1},
		{"o trecho reservado ficou cheio", "Feira", "Salvador", 0},
		{"a viagem inteira precisa dos dois", "Feira", "Ilheus", 0},
	}

	for _, caso := range casos {
		opcoes := b.Buscar(caso.origem, caso.destino, "2026-09-14")
		if len(opcoes) != caso.querOpcoes {
			t.Errorf("%s: %s->%s deu %d opcao(oes), queria %d",
				caso.nome, caso.origem, caso.destino, len(opcoes), caso.querOpcoes)
		}
	}
}

// TestBuscaSentidoUnico: a carona so anda para frente.
func TestBuscaSentidoUnico(t *testing.T) {
	b, _ := bancoComCarona(t, []string{"Feira", "Salvador", "Ilheus"}, 2)

	if n := len(b.Buscar("Ilheus", "Feira", "2026-09-14")); n != 0 {
		t.Errorf("sentido contrario deveria dar 0 opcoes, deu %d", n)
	}
	if n := len(b.Buscar("Salvador", "Salvador", "2026-09-14")); n != 0 {
		t.Errorf("origem igual ao destino deveria dar 0 opcoes, deu %d", n)
	}
	if n := len(b.Buscar("Feira", "Recife", "2026-09-14")); n != 0 {
		t.Errorf("cidade fora da rota deveria dar 0 opcoes, deu %d", n)
	}
	if n := len(b.Buscar("Feira", "Ilheus", "2026-12-25")); n != 0 {
		t.Errorf("data diferente deveria dar 0 opcoes, deu %d", n)
	}
}

// TestBuscaComConexao: duas caronas que juntas levam o passageiro ao destino.
func TestBuscaComConexao(t *testing.T) {
	b, _ := bancoComCarona(t, []string{"Feira", "Salvador"}, 2)

	if _, err := b.CadastrarCarona("davi", []string{"Salvador", "Ilheus"}, "2026-09-14", 2, 40); err != nil {
		t.Fatalf("nao consegui cadastrar: %v", err)
	}

	opcoes := b.Buscar("Feira", "Ilheus", "2026-09-14")
	if len(opcoes) != 1 {
		t.Fatalf("esperava 1 opcao com conexao, deu %d", len(opcoes))
	}

	if n := len(opcoes[0].Itens); n != 2 {
		t.Errorf("a opcao deveria ter 2 itens, tem %d", n)
	}

	// 30 do primeiro trecho + 40 do segundo.
	if opcoes[0].Preco != 70 {
		t.Errorf("preco esperado 70, deu %d", opcoes[0].Preco)
	}
}

// ---------------------------------------------------------------------------
// Expiracao e pagamento
// ---------------------------------------------------------------------------

// TestExpiracaoDevolveAssento: quem nao paga perde o assento.
func TestExpiracaoDevolveAssento(t *testing.T) {
	original := TempoDeReserva
	TempoDeReserva = time.Millisecond
	defer func() { TempoDeReserva = original }()

	b, id := bancoComCarona(t, []string{"Feira", "Salvador"}, 1)

	if _, err := b.Reservar("bruno", trecho(id, 0, 1)); err != nil {
		t.Fatalf("deveria funcionar: %v", err)
	}

	// Antes de expirar, ninguem mais consegue.
	if _, err := b.Reservar("carla", trecho(id, 0, 1)); err == nil {
		t.Fatal("carla nao deveria conseguir: o assento esta ocupado")
	}

	time.Sleep(5 * time.Millisecond)

	if n := b.ExpirarVencidas(); n != 1 {
		t.Fatalf("esperava 1 reserva expirada, deu %d", n)
	}

	// Agora sim.
	if _, err := b.Reservar("carla", trecho(id, 0, 1)); err != nil {
		t.Errorf("carla deveria conseguir depois da expiracao: %v", err)
	}
}

// TestReservaPagaNaoExpira: pagou, o assento e seu para sempre.
func TestReservaPagaNaoExpira(t *testing.T) {
	original := TempoDeReserva
	TempoDeReserva = time.Millisecond
	defer func() { TempoDeReserva = original }()

	b, id := bancoComCarona(t, []string{"Feira", "Salvador"}, 1)

	reserva, err := b.Reservar("bruno", trecho(id, 0, 1))
	if err != nil {
		t.Fatalf("deveria funcionar: %v", err)
	}

	if err := b.Pagar("bruno", reserva); err != nil {
		t.Fatalf("o pagamento deveria funcionar: %v", err)
	}

	time.Sleep(5 * time.Millisecond)

	if n := b.ExpirarVencidas(); n != 0 {
		t.Errorf("reserva paga nao pode expirar, mas expirou %d", n)
	}

	if _, err := b.Reservar("carla", trecho(id, 0, 1)); err == nil {
		t.Error("o assento deveria continuar ocupado")
	}
}

// TestRegrasDePagamento cobre os erros possiveis ao pagar.
func TestRegrasDePagamento(t *testing.T) {
	b, id := bancoComCarona(t, []string{"Feira", "Salvador"}, 2)

	reserva, err := b.Reservar("bruno", trecho(id, 0, 1))
	if err != nil {
		t.Fatalf("deveria funcionar: %v", err)
	}

	if err := b.Pagar("carla", reserva); err == nil {
		t.Error("carla nao deveria pagar reserva do bruno")
	}
	if err := b.Pagar("bruno", 999); err == nil {
		t.Error("reserva inexistente deveria dar erro")
	}
	if err := b.Pagar("bruno", reserva); err != nil {
		t.Errorf("o dono deveria conseguir pagar: %v", err)
	}
	if err := b.Pagar("bruno", reserva); err == nil {
		t.Error("pagar duas vezes deveria dar erro")
	}
}

// ---------------------------------------------------------------------------
// Login e cadastro
// ---------------------------------------------------------------------------

func TestAutenticar(t *testing.T) {
	b := NovoBanco()

	casos := []struct {
		nome, login, senha string
		quer               bool
	}{
		{"usuario e senha certos", "ana", "123", true},
		{"senha errada", "ana", "outra", false},
		{"usuario inexistente", "ninguem", "123", false},
		{"senha vazia", "ana", "", false},
	}

	for _, caso := range casos {
		if _, ok := b.Autenticar(caso.login, caso.senha); ok != caso.quer {
			t.Errorf("%s: Autenticar(%q, %q) deu %v, queria %v",
				caso.nome, caso.login, caso.senha, ok, caso.quer)
		}
	}
}

func TestCadastrarUsuario(t *testing.T) {
	b := NovoBanco()

	if err := b.CadastrarUsuario("henrick", "senha1", "motorista"); err != nil {
		t.Fatalf("deveria funcionar: %v", err)
	}

	u, ok := b.Autenticar("henrick", "senha1")
	if !ok {
		t.Fatal("deveria conseguir entrar com a conta recem-criada")
	}
	if u.Tipo != "motorista" {
		t.Errorf("tipo esperado motorista, deu %q", u.Tipo)
	}

	casos := []struct {
		nome, login, senha, tipo string
	}{
		{"login repetido", "henrick", "outra", "passageiro"},
		{"login que ja vem no banco", "ana", "x", "passageiro"},
		{"login vazio", "", "senha", "passageiro"},
		{"login so com espacos", "   ", "senha", "passageiro"},
		{"senha vazia", "novo", "", "passageiro"},
		{"tipo invalido", "novo", "senha", "piloto"},
		{"tipo vazio", "novo", "senha", ""},
	}

	for _, caso := range casos {
		if err := b.CadastrarUsuario(caso.login, caso.senha, caso.tipo); err == nil {
			t.Errorf("%s: deveria dar erro, mas passou", caso.nome)
		}
	}

	// O cadastro repetido nao pode ter trocado a senha do dono original.
	if _, ok := b.Autenticar("henrick", "senha1"); !ok {
		t.Error("a tentativa repetida sobrescreveu o usuario original")
	}
}

// TestCadastroConcorrente: 500 goroutines tentando criar O MESMO login.
// So uma pode vencer. Se a checagem "ja existe" estivesse fora do Lock,
// varias passariam pela verificacao antes de qualquer uma escrever.
func TestCadastroConcorrente(t *testing.T) {
	const tentativas = 500

	b := NovoBanco()

	sucessos := make(chan int, tentativas)
	largada := make(chan struct{})
	var wg sync.WaitGroup

	for i := 0; i < tentativas; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-largada
			if err := b.CadastrarUsuario("disputado", "senha", "passageiro"); err == nil {
				sucessos <- 1
			}
		}()
	}

	close(largada)
	wg.Wait()
	close(sucessos)

	if n := len(sucessos); n != 1 {
		t.Errorf("esperava exatamente 1 cadastro bem sucedido, deu %d", n)
	}
}

// TestCadastrosSimultaneos e o par do TestContagemDeAssentos, agora para o
// mapa de usuarios: 300 logins DIFERENTES criados ao mesmo tempo.
//
// Como cada goroutine escreve mesmo (nenhuma e recusada), todas mexem no mapa
// em paralelo. Sem o Lock, o proprio runtime do Go derruba o programa com
// "fatal error: concurrent map writes" -- mapa nao suporta escrita simultanea.
func TestCadastrosSimultaneos(t *testing.T) {
	const quantos = 300

	b := NovoBanco()
	iniciais := len(b.Usuarios)

	var wg sync.WaitGroup
	largada := make(chan struct{})

	for i := 0; i < quantos; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			<-largada
			login := fmt.Sprintf("usuario%d", n)
			if err := b.CadastrarUsuario(login, "senha", "passageiro"); err != nil {
				t.Errorf("cadastro de %s falhou: %v", login, err)
			}
		}(i)
	}

	close(largada)
	wg.Wait()

	if n := len(b.Usuarios); n != iniciais+quantos {
		t.Errorf("esperava %d usuarios, deu %d", iniciais+quantos, n)
	}
}

func TestCadastroInvalido(t *testing.T) {
	b := NovoBanco()

	casos := []struct {
		nome     string
		rota     []string
		data     string
		assentos int
	}{
		{"rota com uma cidade so", []string{"Feira"}, "2026-09-14", 2},
		{"rota vazia", nil, "2026-09-14", 2},
		{"sem assentos", []string{"Feira", "Salvador"}, "2026-09-14", 0},
		{"sem data", []string{"Feira", "Salvador"}, "", 2},
	}

	for _, caso := range casos {
		if _, err := b.CadastrarCarona("ana", caso.rota, caso.data, caso.assentos, 30); err == nil {
			t.Errorf("%s: deveria dar erro, mas passou", caso.nome)
		}
	}
}

// TestOcupadosNasceZerado: uma rota de N cidades tem N-1 contadores.
func TestOcupadosNasceZerado(t *testing.T) {
	b, id := bancoComCarona(t, []string{"Feira", "Salvador", "Ilheus", "Porto Seguro"}, 3)

	c := b.acharCarona(id)
	if len(c.Ocupados) != 3 {
		t.Fatalf("rota de 4 cidades deveria ter 3 trechos, tem %d", len(c.Ocupados))
	}

	for i, n := range c.Ocupados {
		if n != 0 {
			t.Errorf("trecho %d deveria nascer zerado, esta em %d", i, n)
		}
	}
}
