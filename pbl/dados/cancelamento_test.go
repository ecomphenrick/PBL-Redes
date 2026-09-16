package dados

import (
	"strings"
	"sync"
	"testing"
	"time"

	"vaijunto/protocolo"
)

// ---------------------------------------------------------------------------
// Cancelar reserva
// ---------------------------------------------------------------------------

func TestCancelarReservaDevolveAssento(t *testing.T) {
	b, id := bancoComCarona(t, []string{"Feira", "Salvador", "Ilheus"}, 1)

	reserva, err := b.Reservar("bruno", trecho(id, 0, 2))
	if err != nil {
		t.Fatalf("deveria funcionar: %v", err)
	}

	if err := b.CancelarReserva("bruno", reserva); err != nil {
		t.Fatalf("o cancelamento deveria funcionar: %v", err)
	}

	for i, n := range b.acharCarona(id).Ocupados {
		if n != 0 {
			t.Errorf("trecho %d deveria voltar a 0, esta em %d", i, n)
		}
	}

	if _, err := b.Reservar("carla", trecho(id, 0, 2)); err != nil {
		t.Errorf("carla deveria conseguir o assento devolvido: %v", err)
	}
}

func TestRegrasDeCancelarReserva(t *testing.T) {
	original := TempoDeReserva
	// Prazo curto, mas com folga: Reservar e Pagar agora expiram o que venceu
	// na hora, entao 1ms poderia vencer entre uma linha e outra do teste.
	TempoDeReserva = 50 * time.Millisecond
	defer func() { TempoDeReserva = original }()

	b, id := bancoComCarona(t, []string{"Feira", "Salvador"}, 3)

	paga, _ := b.Reservar("bruno", trecho(id, 0, 1))
	if err := b.Pagar("bruno", paga); err != nil {
		t.Fatalf("o pagamento deveria funcionar: %v", err)
	}

	vencida, _ := b.Reservar("bruno", trecho(id, 0, 1))
	time.Sleep(80 * time.Millisecond)
	b.ExpirarVencidas()

	if err := b.CancelarReserva("carla", paga); err == nil {
		t.Error("carla nao pode cancelar reserva do bruno")
	}
	if err := b.CancelarReserva("bruno", 999); err == nil {
		t.Error("reserva inexistente deveria dar erro")
	}
	if err := b.CancelarReserva("bruno", vencida); err == nil {
		t.Error("reserva expirada nao pode ser cancelada")
	}
	if err := b.CancelarReserva("bruno", paga); err != nil {
		t.Errorf("reserva paga pode ser cancelada: %v", err)
	}
	if err := b.CancelarReserva("bruno", paga); err == nil {
		t.Error("cancelar duas vezes deveria dar erro")
	}
	if err := b.Pagar("bruno", paga); err == nil {
		t.Error("reserva cancelada nao pode ser paga")
	}

	// A expirada ja tinha devolvido o assento dela. Se o cancelamento
	// devolvesse de novo, o contador ficaria NEGATIVO.
	if n := b.acharCarona(id).Ocupados[0]; n != 0 {
		t.Errorf("esperava 0 assentos ocupados, deu %d", n)
	}
}

// ---------------------------------------------------------------------------
// Cancelar carona
// ---------------------------------------------------------------------------

// TestCancelarCaronaCancelaConexaoInteira e o caso que exige cuidado: a
// reserva do bruno usa duas caronas. Quando uma delas e cancelada, a reserva
// inteira cai, e o assento dele na OUTRA carona tambem precisa voltar.
func TestCancelarCaronaCancelaConexaoInteira(t *testing.T) {
	b, primeira := bancoComCarona(t, []string{"Feira", "Salvador"}, 2)

	segunda, err := b.CadastrarCarona("davi", []string{"Salvador", "Ilheus"}, "2026-09-14", 2, 40)
	if err != nil {
		t.Fatalf("nao consegui cadastrar: %v", err)
	}

	conexao, err := b.Reservar("bruno", []protocolo.Item{
		{CaronaID: primeira, De: 0, Ate: 1},
		{CaronaID: segunda, De: 0, Ate: 1},
	})
	if err != nil {
		t.Fatalf("a reserva com conexao deveria funcionar: %v", err)
	}

	direta, err := b.Reservar("carla", trecho(primeira, 0, 1))
	if err != nil {
		t.Fatalf("a reserva direta deveria funcionar: %v", err)
	}

	afetadas, err := b.CancelarCarona("davi", segunda)
	if err != nil {
		t.Fatalf("o cancelamento deveria funcionar: %v", err)
	}
	if afetadas != 1 {
		t.Errorf("esperava 1 reserva afetada, deu %d", afetadas)
	}

	if r := b.acharReserva(conexao); r.Estado != "cancelada" {
		t.Errorf("a reserva com conexao deveria estar cancelada, esta %q", r.Estado)
	}
	if r := b.acharReserva(direta); r.Estado != "pendente" {
		t.Errorf("a reserva da carla nao usa a carona cancelada, deveria seguir pendente, esta %q", r.Estado)
	}

	// Na primeira carona so pode sobrar o assento da carla.
	if n := b.acharCarona(primeira).Ocupados[0]; n != 1 {
		t.Errorf("a primeira carona deveria ter 1 ocupado (so a carla), tem %d", n)
	}

	// Carona cancelada some da busca e recusa reserva nova.
	if n := len(b.Buscar("Salvador", "Ilheus", "2026-09-14")); n != 0 {
		t.Errorf("carona cancelada nao deveria aparecer na busca, apareceu %d vez(es)", n)
	}
	if _, err := b.Reservar("carla", trecho(segunda, 0, 1)); err == nil {
		t.Error("nao deveria ser possivel reservar carona cancelada")
	}
}

func TestRegrasDeCancelarCarona(t *testing.T) {
	b, id := bancoComCarona(t, []string{"Feira", "Salvador"}, 2)

	if _, err := b.CancelarCarona("davi", id); err == nil {
		t.Error("davi nao e dono da carona e nao pode cancelar")
	}
	if _, err := b.CancelarCarona("ana", 999); err == nil {
		t.Error("carona inexistente deveria dar erro")
	}
	if _, err := b.CancelarCarona("ana", id); err != nil {
		t.Errorf("a dona deveria conseguir cancelar: %v", err)
	}
	if _, err := b.CancelarCarona("ana", id); err == nil {
		t.Error("cancelar duas vezes deveria dar erro")
	}
}

// ---------------------------------------------------------------------------
// Passageiros por trecho
// ---------------------------------------------------------------------------

func TestPassageirosPorTrecho(t *testing.T) {
	b, id := bancoComCarona(t, []string{"Feira", "Salvador", "Ilheus"}, 2)

	if _, err := b.Reservar("bruno", trecho(id, 0, 2)); err != nil { // os dois trechos
		t.Fatalf("deveria funcionar: %v", err)
	}

	carla, err := b.Reservar("carla", trecho(id, 1, 2)) // so o segundo
	if err != nil {
		t.Fatalf("deveria funcionar: %v", err)
	}
	b.Pagar("carla", carla)

	linhas, err := b.PassageirosDaCarona("ana", id)
	if err != nil {
		t.Fatalf("a dona deveria ver os passageiros: %v", err)
	}

	// linhas[0] e o cabecalho; depois vem uma linha por trecho.
	if len(linhas) != 3 {
		t.Fatalf("esperava cabecalho + 2 trechos, deu %d linhas: %v", len(linhas), linhas)
	}

	primeiro, segundo := linhas[1], linhas[2]

	if !strings.Contains(primeiro, "bruno") || strings.Contains(primeiro, "carla") {
		t.Errorf("Feira->Salvador deveria ter so o bruno: %q", primeiro)
	}
	if !strings.Contains(segundo, "bruno") || !strings.Contains(segundo, "carla (paga)") {
		t.Errorf("Salvador->Ilheus deveria ter bruno e carla (paga): %q", segundo)
	}

	if _, err := b.PassageirosDaCarona("davi", id); err == nil {
		t.Error("so a dona da carona pode ver os passageiros")
	}
}

// ---------------------------------------------------------------------------
// Concorrencia nos cancelamentos
// ---------------------------------------------------------------------------

// TestReservaECancelamentoConcorrentes: 1000 goroutines reservam e cancelam ao
// mesmo tempo. Todas escrevem DUAS vezes (ocupar e liberar), entao se o mutex
// faltar em qualquer das duas funcoes, incrementos e decrementos se perdem e
// o contador final nao volta a zero.
func TestReservaECancelamentoConcorrentes(t *testing.T) {
	const pares = 1000

	b, id := bancoComCarona(t, []string{"Feira", "Salvador"}, pares)

	var wg sync.WaitGroup
	largada := make(chan struct{})

	for i := 0; i < pares; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-largada

			reserva, err := b.Reservar("bruno", trecho(id, 0, 1))
			if err != nil {
				t.Errorf("a reserva deveria funcionar: %v", err)
				return
			}
			if err := b.CancelarReserva("bruno", reserva); err != nil {
				t.Errorf("o cancelamento deveria funcionar: %v", err)
			}
		}()
	}

	close(largada)
	wg.Wait()

	if n := b.acharCarona(id).Ocupados[0]; n != 0 {
		t.Errorf("tudo foi cancelado, mas sobraram %d assentos ocupados", n)
	}
}

// TestCancelarCaronaDuranteReservas: o motorista cancela enquanto 500
// passageiros tentam reservar. Nao importa quem chega primeiro; o que nao pode
// acontecer e sobrar reserva ativa numa carona cancelada.
func TestCancelarCaronaDuranteReservas(t *testing.T) {
	const pedidos = 500

	b, id := bancoComCarona(t, []string{"Feira", "Salvador"}, pedidos)

	var wg sync.WaitGroup
	largada := make(chan struct{})

	for i := 0; i < pedidos; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-largada
			b.Reservar("bruno", trecho(id, 0, 1)) // pode falhar se o cancelamento vier antes
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-largada
		if _, err := b.CancelarCarona("ana", id); err != nil {
			t.Errorf("o cancelamento deveria funcionar: %v", err)
		}
	}()

	close(largada)
	wg.Wait()

	for _, r := range b.Reservas {
		if r.ativa() {
			t.Errorf("reserva %d continua %s numa carona cancelada", r.ID, r.Estado)
		}
	}

	if n := b.acharCarona(id).Ocupados[0]; n != 0 {
		t.Errorf("carona cancelada deveria terminar sem assentos ocupados, tem %d", n)
	}
}
