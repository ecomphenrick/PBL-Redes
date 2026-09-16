// Programa de teste de carga: N clientes REAIS, cada um com a sua conexao TCP,
// disputando os mesmos assentos ao mesmo tempo contra o servidor de verdade.
//
// Os testes do pacote dados chamam as funcoes direto. Aqui tudo passa pela
// rede: socket, protocolo JSON, goroutine por conexao no servidor e o mutex
// do Banco. E o teste que mostra o sistema inteiro aguentando concorrencia.
//
// Com o servidor ja rodando:
//
//	go run ./carga
//	go run ./carga -clientes 500 -assentos 25
//	go run ./carga -servidor 192.168.0.10:8080
package main

import (
	"bufio"
	"flag"
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	"vaijunto/protocolo"
)

func main() {
	padrao := "localhost:8080"
	if v := os.Getenv("VAIJUNTO_SERVIDOR"); v != "" {
		padrao = v
	}

	endereco := flag.String("servidor", padrao, "endereco do servidor")
	clientes := flag.Int("clientes", 200, "quantos clientes disputam ao mesmo tempo")
	assentos := flag.Int("assentos", 10, "lugares da carona mais apertada do itinerario")
	flag.Parse()

	if err := rodar(*endereco, *clientes, *assentos); err != nil {
		fmt.Println("\nFALHOU:", err)
		os.Exit(1)
	}

	fmt.Println("\nPASSOU")
}

// ---------------------------------------------------------------------------
// O cenario
// ---------------------------------------------------------------------------

func rodar(endereco string, clientes, assentos int) error {
	// Sufixo unico: a data e os logins desta rodada nao colidem com nada que
	// ja exista no servidor, entao o teste pode rodar varias vezes seguidas.
	tag := strconv.FormatInt(time.Now().UnixNano(), 36)
	data := "carga-" + tag

	fmt.Printf("servidor %s | %d clientes | %d lugares disputados\n\n", endereco, clientes, assentos)

	// ---- 1. O motorista monta o cenario ----

	motorista, err := conectar(endereco)
	if err != nil {
		return fmt.Errorf("o motorista nao conectou: %w", err)
	}
	defer motorista.Close()

	if err := entrar(motorista, "mot_"+tag, "motorista"); err != nil {
		return err
	}

	// A primeira carona tem o DOBRO de lugares da segunda. Se a atomicidade
	// falhar -- uma reserva ocupar a primeira sem ocupar a segunda -- sobra
	// lugar ocupado na primeira sem par, e a verificacao do passo 4 enxerga.
	a, err := motorista.exigir(protocolo.Pedido{
		Acao: "cadastrar", Rota: []string{"Feira", "Salvador"},
		Data: data, Assentos: 2 * assentos, Preco: 30,
	})
	if err != nil {
		return err
	}

	b, err := motorista.exigir(protocolo.Pedido{
		Acao: "cadastrar", Rota: []string{"Salvador", "Ilheus"},
		Data: data, Assentos: assentos, Preco: 40,
	})
	if err != nil {
		return err
	}

	// O itinerario disputado usa as DUAS caronas: tudo ou nada.
	itinerario := []protocolo.Item{
		{CaronaID: a.CaronaID, De: 0, Ate: 1},
		{CaronaID: b.CaronaID, De: 0, Ate: 1},
	}

	fmt.Printf("itinerario: carona %d Feira->Salvador (%d lugares) + carona %d Salvador->Ilheus (%d lugares)\n",
		a.CaronaID, 2*assentos, b.CaronaID, assentos)

	// ---- 2. Os clientes conectam, fazem login e disparam juntos ----

	resultados := make(chan resultado, clientes)
	largada := make(chan struct{})

	// A preparacao (conectar + login) roda com no maximo 50 clientes por vez.
	//
	// O que este teste mede e a DISPUTA pela reserva, e ela continua com todos
	// juntos, depois da largada. Ja abrir centenas de conexoes no mesmo
	// milissegundo so estoura a fila de conexoes pendentes do sistema
	// operacional, que tem tamanho limitado e recusa o excedente antes de o
	// servidor sequer ver a conexao.
	preparando := make(chan struct{}, 50)

	var prontos, terminados sync.WaitGroup

	for i := 0; i < clientes; i++ {
		prontos.Add(1)
		terminados.Add(1)

		go func(n int) {
			defer terminados.Done()

			// Preparacao: cada cliente tem a SUA conexao e o SEU usuario.
			preparando <- struct{}{} // pega uma das 50 vagas
			c, err := conectar(endereco)
			if err == nil {
				defer c.Close()
				err = entrar(c, fmt.Sprintf("pas_%s_%d", tag, n), "passageiro")
			}
			<-preparando // devolve a vaga

			prontos.Done()
			<-largada // espera todos estarem prontos

			if err != nil {
				resultados <- resultado{erro: err}
				return
			}

			inicio := time.Now()
			r, err := c.pedir(protocolo.Pedido{Acao: "reservar", Itens: itinerario})
			latencia := time.Since(inicio)

			switch {
			case err != nil:
				resultados <- resultado{erro: err}
			case !r.OK:
				resultados <- resultado{recusado: true, latencia: latencia}
			default:
				resultados <- resultado{reservaID: r.ReservaID, latencia: latencia}
			}
		}(i)
	}

	prontos.Wait()
	fmt.Printf("%d clientes conectados e logados; disparando todos juntos...\n\n", clientes)

	inicio := time.Now()
	close(largada)
	terminados.Wait()
	duracao := time.Since(inicio)
	close(resultados)

	// ---- 3. Contagem ----

	var latencias []time.Duration
	ids := map[int]bool{}
	aceitas, recusadas, falhas := 0, 0, 0
	var primeiraFalha error

	for r := range resultados {
		switch {
		case r.erro != nil:
			falhas++
			if primeiraFalha == nil {
				primeiraFalha = r.erro
			}
			continue
		case r.recusado:
			recusadas++
		default:
			aceitas++
			ids[r.reservaID] = true
		}
		latencias = append(latencias, r.latencia)
	}

	fmt.Printf("reservas aceitas:   %d\n", aceitas)
	fmt.Printf("reservas recusadas: %d (sem lugar: e o esperado)\n", recusadas)
	fmt.Printf("falhas de conexao:  %d\n", falhas)
	if primeiraFalha != nil {
		fmt.Println("  primeira falha:", primeiraFalha)
	}
	fmt.Printf("tempo da disputa:   %s\n", duracao.Round(time.Millisecond))
	imprimirLatencias(latencias)

	// ---- 4. Verificacao ----

	fmt.Println("\nverificacao:")
	v := &verificacao{}

	v.conferir("nenhum lugar vendido duas vezes",
		aceitas <= assentos,
		fmt.Sprintf("%d reservas aceitas para %d lugares", aceitas, assentos))

	esperadas := min(assentos, clientes-falhas)
	v.conferir("todos os lugares disponiveis foram vendidos",
		aceitas == esperadas,
		fmt.Sprintf("esperava %d, vieram %d", esperadas, aceitas))

	v.conferir("cada reserva aceita tem numero unico",
		len(ids) == aceitas,
		fmt.Sprintf("%d reservas mas so %d numeros diferentes", aceitas, len(ids)))

	// Atomicidade: uma sonda conta, pelo proprio protocolo, quantos lugares
	// sobraram em cada carona. Se toda reserva aceita ocupou as DUAS, sobra
	// exatamente (lugares - aceitas) em cada uma.
	sonda, err := conectar(endereco)
	if err != nil {
		return fmt.Errorf("a sonda nao conectou: %w", err)
	}
	defer sonda.Close()

	if err := entrar(sonda, "sonda_"+tag, "passageiro"); err != nil {
		return err
	}

	sobraB, errB := contarVagas(sonda, b.CaronaID, assentos)
	sobraA, errA := contarVagas(sonda, a.CaronaID, 2*assentos)

	v.conferir("nenhum itinerario pela metade",
		errA == nil && errB == nil && sobraA == 2*assentos-aceitas && sobraB == assentos-aceitas,
		fmt.Sprintf("sobraram %d na primeira (esperado %d) e %d na segunda (esperado %d)",
			sobraA, 2*assentos-aceitas, sobraB, assentos-aceitas))

	// Limpeza: cancelar as caronas desfaz tudo que esta rodada criou.
	for _, id := range []int{a.CaronaID, b.CaronaID} {
		motorista.pedir(protocolo.Pedido{Acao: "cancelar_carona", CaronaID: id})
	}

	if v.falhou {
		return fmt.Errorf("alguma verificacao nao passou")
	}
	return nil
}

// contarVagas descobre quantos lugares sobraram numa carona reservando um por
// um ate o servidor recusar. Usa so o protocolo, sem olhar dentro do servidor.
func contarVagas(c *conexao, caronaID, limite int) (int, error) {
	item := []protocolo.Item{{CaronaID: caronaID, De: 0, Ate: 1}}

	for vagas := 0; vagas <= limite; vagas++ {
		r, err := c.pedir(protocolo.Pedido{Acao: "reservar", Itens: item})
		if err != nil {
			return vagas, err
		}
		if !r.OK {
			return vagas, nil
		}
	}

	// Mais vagas do que a carona tem: o estado do servidor esta corrompido.
	return limite + 1, nil
}

// ---------------------------------------------------------------------------
// Pecas de apoio
// ---------------------------------------------------------------------------

// conexao e um cliente minimo: o socket e o leitor com buffer dele.
type conexao struct {
	net.Conn
	leitor *bufio.Reader
}

func conectar(endereco string) (*conexao, error) {
	c, err := net.Dial("tcp", endereco)
	if err != nil {
		return nil, err
	}
	return &conexao{Conn: c, leitor: bufio.NewReader(c)}, nil
}

func (c *conexao) pedir(p protocolo.Pedido) (protocolo.Resposta, error) {
	if err := protocolo.EnviarPedido(c, p); err != nil {
		return protocolo.Resposta{}, err
	}
	return protocolo.LerResposta(c.leitor)
}

// exigir e o pedir das etapas de preparacao: ali uma recusa do servidor
// tambem e erro, porque o teste nao consegue seguir sem aquilo.
func (c *conexao) exigir(p protocolo.Pedido) (protocolo.Resposta, error) {
	r, err := c.pedir(p)
	if err != nil {
		return r, err
	}
	if !r.OK {
		return r, fmt.Errorf("%s recusado: %s", p.Acao, r.Erro)
	}
	return r, nil
}

// entrar cria um usuario novo e faz login com ele na mesma conexao.
func entrar(c *conexao, login, tipo string) error {
	if _, err := c.exigir(protocolo.Pedido{Acao: "registrar", Usuario: login, Senha: "carga", Tipo: tipo}); err != nil {
		return err
	}
	_, err := c.exigir(protocolo.Pedido{Acao: "login", Usuario: login, Senha: "carga"})
	return err
}

type resultado struct {
	reservaID int
	recusado  bool
	erro      error
	latencia  time.Duration
}

type verificacao struct {
	falhou bool
}

func (v *verificacao) conferir(nome string, ok bool, detalhe string) {
	if ok {
		fmt.Printf("  [ok]     %s\n", nome)
		return
	}
	fmt.Printf("  [FALHOU] %s: %s\n", nome, detalhe)
	v.falhou = true
}

func imprimirLatencias(l []time.Duration) {
	if len(l) == 0 {
		return
	}

	sort.Slice(l, func(i, j int) bool { return l[i] < l[j] })

	var soma time.Duration
	for _, d := range l {
		soma += d
	}

	percentil := func(p float64) time.Duration {
		return l[int(p*float64(len(l)-1))]
	}

	arred := func(d time.Duration) time.Duration { return d.Round(100 * time.Microsecond) }

	fmt.Printf("latencia da reserva: min %s | media %s | p50 %s | p95 %s | max %s\n",
		arred(l[0]), arred(soma/time.Duration(len(l))), arred(percentil(0.50)),
		arred(percentil(0.95)), arred(l[len(l)-1]))
}
