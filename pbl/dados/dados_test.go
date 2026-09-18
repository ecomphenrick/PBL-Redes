package dados

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"vaijunto/protocolo"
)

func trecho(caronaID, de, ate int) []protocolo.Item {
	return []protocolo.Item{{CaronaID: caronaID, De: de, Ate: ate}}
}

func bancoComCarona(t *testing.T, rota []string, assentos int) (*Banco, int) {
	t.Helper()

	b := NovoBanco()
	id, err := b.CadastrarCarona("ana", rota, "2026-09-14", assentos, 30)
	if err != nil {
		t.Fatalf("nao consegui cadastrar a carona: %v", err)
	}

	return b, id
}

func TestReservaConcorrente(t *testing.T) {
	const tentativas = 200

	b, id := bancoComCarona(t, []string{"Feira", "Salvador"}, 1)

	sucessos := make(chan int, tentativas)

	var wg sync.WaitGroup

	largada := make(chan struct{})

	for i := 0; i < tentativas; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			<-largada

			if _, err := b.Reservar("bruno", trecho(id, 0, 1)); err == nil {
				sucessos <- 1
			}
		}()
	}

	close(largada)
	wg.Wait()
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

	c := b.acharCarona(id)
	if c.Ocupados[0] < 0 || c.Ocupados[0] > c.Assentos {
		t.Errorf("contador de assentos incoerente: %d (total %d)", c.Ocupados[0], c.Assentos)
	}
}

func TestReservaAtomica(t *testing.T) {
	b, livre := bancoComCarona(t, []string{"Ilheus", "Porto Seguro"}, 2)

	cheia, err := b.CadastrarCarona("davi", []string{"Feira", "Salvador"}, "2026-09-14", 1, 30)
	if err != nil {
		t.Fatalf("nao consegui cadastrar: %v", err)
	}

	if _, err := b.Reservar("carla", trecho(cheia, 0, 1)); err != nil {
		t.Fatalf("a primeira reserva deveria funcionar: %v", err)
	}

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

func TestDisponibilidadePorTrecho(t *testing.T) {
	b, id := bancoComCarona(t, []string{"Feira", "Salvador", "Ilheus"}, 1)

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

	if opcoes[0].Preco != 70 {
		t.Errorf("preco esperado 70, deu %d", opcoes[0].Preco)
	}
}

func TestExpiracaoDevolveAssento(t *testing.T) {
	original := TempoDeReserva

	TempoDeReserva = 50 * time.Millisecond
	defer func() { TempoDeReserva = original }()

	b, id := bancoComCarona(t, []string{"Feira", "Salvador"}, 1)

	if _, err := b.Reservar("bruno", trecho(id, 0, 1)); err != nil {
		t.Fatalf("deveria funcionar: %v", err)
	}

	if _, err := b.Reservar("carla", trecho(id, 0, 1)); err == nil {
		t.Fatal("carla nao deveria conseguir: o assento esta ocupado")
	}

	time.Sleep(80 * time.Millisecond)

	if n := b.ExpirarVencidas(); n != 1 {
		t.Fatalf("esperava 1 reserva expirada, deu %d", n)
	}

	if _, err := b.Reservar("carla", trecho(id, 0, 1)); err != nil {
		t.Errorf("carla deveria conseguir depois da expiracao: %v", err)
	}
}

func TestReservaPagaNaoExpira(t *testing.T) {
	original := TempoDeReserva

	TempoDeReserva = 50 * time.Millisecond
	defer func() { TempoDeReserva = original }()

	b, id := bancoComCarona(t, []string{"Feira", "Salvador"}, 1)

	reserva, err := b.Reservar("bruno", trecho(id, 0, 1))
	if err != nil {
		t.Fatalf("deveria funcionar: %v", err)
	}

	if err := b.Pagar("bruno", reserva); err != nil {
		t.Fatalf("o pagamento deveria funcionar: %v", err)
	}

	time.Sleep(80 * time.Millisecond)

	if n := b.ExpirarVencidas(); n != 0 {
		t.Errorf("reserva paga nao pode expirar, mas expirou %d", n)
	}

	if _, err := b.Reservar("carla", trecho(id, 0, 1)); err == nil {
		t.Error("o assento deveria continuar ocupado")
	}
}

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

func TestAutenticar(t *testing.T) {
	b := NovoBanco()

	if err := b.CadastrarUsuario("ana", "123", "motorista"); err != nil {
		t.Fatalf("nao consegui criar a conta de teste: %v", err)
	}

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

	if _, ok := b.Autenticar("henrick", "senha1"); !ok {
		t.Error("a tentativa repetida sobrescreveu o usuario original")
	}
}

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

func TestVencidaExpiraSemEsperarVarredura(t *testing.T) {
	original := TempoDeReserva
	TempoDeReserva = 50 * time.Millisecond
	defer func() { TempoDeReserva = original }()

	b, id := bancoComCarona(t, []string{"Feira", "Salvador"}, 1)

	reserva, err := b.Reservar("bruno", trecho(id, 0, 1))
	if err != nil {
		t.Fatalf("deveria funcionar: %v", err)
	}

	time.Sleep(80 * time.Millisecond)

	if err := b.Pagar("bruno", reserva); err == nil {
		t.Error("reserva vencida nao pode ser paga")
	}
	if _, err := b.Reservar("carla", trecho(id, 0, 1)); err != nil {
		t.Errorf("o assento da reserva vencida deveria estar livre: %v", err)
	}
}

func TestDataInvalida(t *testing.T) {
	validas := []string{"2026-09-20", "2028-02-29"}
	invalidas := []string{"", "20/09/2026", "2026-9-20", "amanha", "2026-02-30", "2026-13-01"}

	for _, d := range validas {
		if !DataValida(d) {
			t.Errorf("%q deveria ser valida", d)
		}
	}
	for _, d := range invalidas {
		if DataValida(d) {
			t.Errorf("%q deveria ser invalida", d)
		}
	}

	b := NovoBanco()
	if _, err := b.CadastrarCarona("davi", []string{"Feira", "Salvador"}, "20/09/2026", 2, 30); err == nil {
		t.Error("cadastro com data fora do formato deveria dar erro")
	}
}

func TestOrdemDaBusca(t *testing.T) {
	b := NovoBanco()
	cadastrar := func(motorista string, rota []string, preco int) {
		t.Helper()
		if _, err := b.CadastrarCarona(motorista, rota, "2026-09-14", 2, preco); err != nil {
			t.Fatalf("nao consegui cadastrar: %v", err)
		}
	}

	cadastrar("ana", []string{"Feira", "Salvador"}, 10)
	cadastrar("davi", []string{"Salvador", "Ilheus"}, 10)
	cadastrar("ana", []string{"Feira", "Ilheus"}, 90)
	cadastrar("davi", []string{"Feira", "Salvador", "Ilheus"}, 30)

	opcoes := b.Buscar("Feira", "Ilheus", "2026-09-14")
	if len(opcoes) < 3 {
		t.Fatalf("esperava pelo menos 3 opcoes, deu %d", len(opcoes))
	}

	if len(opcoes[0].Itens) != 1 || opcoes[0].Preco != 60 {
		t.Errorf("1a opcao deveria ser a direta de R$60, deu %d item(s) R$%d", len(opcoes[0].Itens), opcoes[0].Preco)
	}
	if len(opcoes[1].Itens) != 1 || opcoes[1].Preco != 90 {
		t.Errorf("2a opcao deveria ser a direta de R$90, deu %d item(s) R$%d", len(opcoes[1].Itens), opcoes[1].Preco)
	}
	for i := 2; i < len(opcoes); i++ {
		if len(opcoes[i].Itens) != 2 {
			t.Errorf("opcao %d deveria ser conexao", i+1)
		}
		if i > 2 && opcoes[i].Preco < opcoes[i-1].Preco {
			t.Errorf("conexoes fora de ordem de preco: R$%d depois de R$%d", opcoes[i].Preco, opcoes[i-1].Preco)
		}
	}
}
