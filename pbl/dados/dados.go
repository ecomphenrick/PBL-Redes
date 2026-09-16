package dados

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"vaijunto/protocolo"
)

// 10 min de reserva.
var TempoDeReserva = 10 * time.Minute

// Usuario e quem usa o sistema. Tipo e "motorista" ou "passageiro".
type Usuario struct {
	Login string `json:"login"`
	Senha string `json:"senha"`
	Tipo  string `json:"tipo"`
}

type Carona struct {
	ID        int      `json:"id"`
	Motorista string   `json:"motorista"`
	Rota      []string `json:"rota"`
	Data      string   `json:"data"`
	Assentos  int      `json:"assentos"` // total de assentos, igual em todo trecho
	Preco     int      `json:"preco"`    // preco POR TRECHO
	Ocupados  []int    `json:"ocupados"` // ocupador por trecho
	Cancelada bool     `json:"cancelada,omitempty"`
}

// Reserva e um pedido de assentos, que pode cobrir varias caronas.
type Reserva struct {
	ID         int              `json:"id"`
	Passageiro string           `json:"passageiro"`
	Itens      []protocolo.Item `json:"itens"`
	Estado     string           `json:"estado"` // "pendente" | "paga" | "expirada" | "cancelada"
	Motivo     string           `json:"motivo,omitempty"`
	Expira     time.Time        `json:"expira"`
}

type Banco struct { //estado do sistema em memória
	mu      sync.Mutex // lock e unlock para entrar na goroutine
	arquivo string     //caminho do arquivo de persistência

	Usuarios map[string]Usuario `json:"usuarios"`
	Caronas  []Carona           `json:"caronas"`
	Reservas []Reserva          `json:"reservas"`
	ProxID   int                `json:"prox_id"`
}

// cria um banco novo
func NovoBanco() *Banco {
	return &Banco{
		Usuarios: map[string]Usuario{},
		ProxID:   1,
	}
}

func Carregar(caminho string) *Banco {
	b := NovoBanco()    //cria um banco novo
	b.arquivo = caminho //tenta recuperar os dados do arquivo

	conteudo, err := os.ReadFile(caminho) //le o arquivo
	if err != nil {
		fmt.Printf("sem dados anteriores em %s, comecando do zero\n", caminho)
		return b
	}

	if err := json.Unmarshal(conteudo, b); err != nil {
		fmt.Printf("arquivo %s ilegivel (%v), comecando do zero\n", caminho, err)
		return NovoBancoEm(caminho)
	}

	fmt.Printf("dados carregados de %s: %d carona(s), %d reserva(s)\n",
		caminho, len(b.Caronas), len(b.Reservas))
	return b
}

// NovoBancoEm cria um banco vazio que grava no caminho indicado.
func NovoBancoEm(caminho string) *Banco {
	b := NovoBanco()
	b.arquivo = caminho
	return b
}

func (b *Banco) Autenticar(login, senha string) (Usuario, bool) {
	b.mu.Lock()         //trava o banco
	defer b.mu.Unlock() //garante que abre no fim da função

	u, existe := b.Usuarios[login] //procura o user no mapa
	if !existe {
		return Usuario{}, false
	}

	if u.Senha != senha {
		return Usuario{}, false
	}

	return u, true
}

// banco chama cadastrar
func (b *Banco) CadastrarUsuario(login, senha, tipo string) error {
	login = strings.TrimSpace(login)

	if login == "" {
		return errors.New("informe o usuario")
	}
	if senha == "" {
		return errors.New("informe a senha")
	}
	if tipo != "motorista" && tipo != "passageiro" {
		return errors.New(`o tipo precisa ser "motorista" ou "passageiro"`)
	}

	//apos conferir formato, trava o banco

	b.mu.Lock()
	defer b.mu.Unlock()

	if _, existe := b.Usuarios[login]; existe { //confere se o usuário já existe
		return fmt.Errorf("o usuario %q ja existe", login)
	}

	b.Usuarios[login] = Usuario{Login: login, Senha: senha, Tipo: tipo} //coloca no dicionario com a chave login
	b.persistir()

	return nil
}

// CadastrarCarona registra uma carona nova e devolve o ID dela.
func (b *Banco) CadastrarCarona(motorista string, rota []string, data string, assentos, preco int) (int, error) {
	// A validacao vem ANTES do Lock: nao faz sentido segurar a fechadura
	// (e travar todos os outros clientes) para conferir argumento.
	if len(rota) < 2 {
		return 0, errors.New("a rota precisa de pelo menos 2 cidades")
	}
	if assentos < 1 {
		return 0, errors.New("a carona precisa de pelo menos 1 assento")
	}
	if data == "" {
		return 0, errors.New("informe a data")
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	c := Carona{
		ID:        b.ProxID,
		Motorista: motorista,
		Rota:      rota,
		Data:      data,
		Assentos:  assentos,
		Preco:     preco,
		Ocupados:  make([]int, len(rota)-1), // um contador por trecho, zerado
	}

	b.ProxID++
	b.Caronas = append(b.Caronas, c)
	b.persistir()

	return c.ID, nil
}

// Buscar devolve as opcoes de viagem de origem ate destino naquela data.
// Primeiro as caronas diretas, depois as que exigem UMA baldeacao.
func (b *Banco) Buscar(origem, destino, data string) []protocolo.Opcao {
	b.mu.Lock()
	defer b.mu.Unlock()

	opcoes := b.buscarDiretas(origem, destino, data)
	return append(opcoes, b.buscarComConexao(origem, destino, data)...)
}

// Reservar segura assentos em uma ou mais caronas, de forma ATOMICA.
//
// Esta e a funcao mais importante do projeto. O enunciado exige que, se o
// passageiro pegar um trecho, ele nao fique sem o outro. A garantia vem de
// duas fases dentro de UM UNICO Lock:
//
//	FASE 1 confere TODOS os itens sem alterar nada.
//	FASE 2 so roda se a fase 1 passou inteira.
//
// Como nada solta o mutex entre as duas, nenhuma outra goroutine consegue se
// intrometer no meio e roubar um assento que acabamos de conferir.
func (b *Banco) Reservar(passageiro string, itens []protocolo.Item) (int, error) {
	if len(itens) == 0 {
		return 0, errors.New("nenhum trecho escolhido")
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// ---- FASE 1: verificacao. NAO altera nada. ----
	for _, item := range itens {
		c := b.acharCarona(item.CaronaID)
		if c == nil {
			return 0, fmt.Errorf("carona %d nao existe", item.CaronaID)
		}
		if c.Cancelada {
			return 0, fmt.Errorf("carona %d foi cancelada", item.CaronaID)
		}
		if item.De < 0 || item.Ate > c.trechos() || item.De >= item.Ate {
			return 0, fmt.Errorf("trecho invalido na carona %d", item.CaronaID)
		}
		if c.livres(item.De, item.Ate) < 1 {
			return 0, fmt.Errorf("sem assento livre na carona %d", item.CaronaID)
		}
	}

	// ---- FASE 2: aplicacao. So chega aqui se TUDO passou. ----
	for _, item := range itens {
		b.acharCarona(item.CaronaID).ocupar(item.De, item.Ate)
	}

	r := Reserva{
		ID:         b.ProxID,
		Passageiro: passageiro,
		Itens:      itens,
		Estado:     "pendente",
		Expira:     time.Now().Add(TempoDeReserva),
	}

	b.ProxID++
	b.Reservas = append(b.Reservas, r)
	b.persistir()

	return r.ID, nil
}

// Pagar confirma uma reserva pendente. Reserva paga nunca mais expira.
func (b *Banco) Pagar(passageiro string, reservaID int) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	r := b.acharReserva(reservaID)
	if r == nil {
		return fmt.Errorf("reserva %d nao existe", reservaID)
	}
	if r.Passageiro != passageiro {
		return errors.New("esta reserva nao e sua")
	}

	switch r.Estado {
	case "paga":
		return errors.New("esta reserva ja foi paga")
	case "expirada":
		return errors.New("esta reserva expirou; os assentos voltaram para a fila")
	case "cancelada":
		return fmt.Errorf("esta reserva foi cancelada (%s)", r.Motivo)
	}

	r.Estado = "paga"
	b.persistir()

	return nil
}

// CancelarReserva desiste de uma reserva pendente ou paga e devolve os
// assentos de TODOS os trechos dela.
//
// E o espelho do Reservar: se a reserva cobre duas caronas, as duas recebem o
// assento de volta juntas, dentro do mesmo Lock.
func (b *Banco) CancelarReserva(passageiro string, reservaID int) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	r := b.acharReserva(reservaID)
	if r == nil {
		return fmt.Errorf("reserva %d nao existe", reservaID)
	}
	if r.Passageiro != passageiro {
		return errors.New("esta reserva nao e sua")
	}
	if !r.ativa() {
		return fmt.Errorf("esta reserva ja esta %s", r.Estado)
	}

	b.cancelar(r, "cancelada pelo passageiro")
	b.persistir()

	return nil
}

// CancelarCarona tira uma carona do ar e cancela todas as reservas ativas que
// passam por ela. Devolve quantas reservas foram afetadas.
//
// Atencao ao caso da conexao: uma reserva Feira->Ilheus pode usar esta carona
// num trecho e OUTRA carona no trecho seguinte. Liberar so a parte desta
// carona deixaria o passageiro com meia viagem -- exatamente o que a
// atomicidade proibe. Por isso a reserva inteira e cancelada, e os assentos
// voltam em TODAS as caronas dela.
func (b *Banco) CancelarCarona(motorista string, caronaID int) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	c := b.acharCarona(caronaID)
	if c == nil {
		return 0, fmt.Errorf("carona %d nao existe", caronaID)
	}
	if c.Motorista != motorista {
		return 0, errors.New("esta carona nao e sua")
	}
	if c.Cancelada {
		return 0, errors.New("esta carona ja foi cancelada")
	}

	c.Cancelada = true

	motivo := fmt.Sprintf("carona %d cancelada pelo motorista", caronaID)
	afetadas := 0

	for i := range b.Reservas {
		r := &b.Reservas[i]
		if r.ativa() && r.usa(caronaID) {
			b.cancelar(r, motivo)
			afetadas++
		}
	}

	b.persistir()

	return afetadas, nil
}

// ExpirarVencidas devolve os assentos das reservas pendentes que passaram do
// prazo. Roda periodicamente numa goroutine do servidor. Devolve quantas
// reservas foram expiradas.
func (b *Banco) ExpirarVencidas() int {
	b.mu.Lock()
	defer b.mu.Unlock()

	agora := time.Now()
	expiradas := 0

	for i := range b.Reservas {
		// Ponteiro para o elemento REAL do slice. Com "for _, r := range"
		// mexeriamos numa copia e o estado nunca mudaria.
		r := &b.Reservas[i]

		if r.Estado != "pendente" || agora.Before(r.Expira) {
			continue
		}

		b.devolverAssentos(r)
		r.Estado = "expirada"
		expiradas++
	}

	if expiradas > 0 {
		b.persistir()
	}

	return expiradas
}

// MinhasCaronas lista as caronas de um motorista, prontas para exibir.
func (b *Banco) MinhasCaronas(motorista string) []string {
	b.mu.Lock()
	defer b.mu.Unlock()

	var linhas []string

	for _, c := range b.Caronas {
		if c.Motorista != motorista {
			continue
		}

		situacao := ""
		if c.Cancelada {
			situacao = " | CANCELADA"
		}

		linhas = append(linhas, fmt.Sprintf("carona %d | %s | %s | R$%d/trecho | ocupados por trecho: %v de %d%s",
			c.ID, c.Data, strings.Join(c.Rota, " -> "), c.Preco, c.Ocupados, c.Assentos, situacao))
	}

	return linhas
}

// PassageirosDaCarona mostra, trecho a trecho, quem esta em cada assento.
// So o motorista dono da carona pode ver.
func (b *Banco) PassageirosDaCarona(motorista string, caronaID int) ([]string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	c := b.acharCarona(caronaID)
	if c == nil {
		return nil, fmt.Errorf("carona %d nao existe", caronaID)
	}
	if c.Motorista != motorista {
		return nil, errors.New("esta carona nao e sua")
	}

	situacao := ""
	if c.Cancelada {
		situacao = " | CANCELADA"
	}

	linhas := []string{fmt.Sprintf("carona %d | %s | %s%s",
		c.ID, c.Data, strings.Join(c.Rota, " -> "), situacao)}

	// Para cada trecho, procura as reservas ativas cujo caminho passa por ele.
	for t := 0; t < c.trechos(); t++ {
		var nomes []string

		for _, r := range b.Reservas {
			if !r.ativa() {
				continue
			}
			for _, item := range r.Itens {
				if item.CaronaID == caronaID && item.De <= t && t < item.Ate {
					nomes = append(nomes, fmt.Sprintf("%s (%s)", r.Passageiro, r.Estado))
				}
			}
		}

		lista := "ninguem"
		if len(nomes) > 0 {
			lista = strings.Join(nomes, ", ")
		}

		linhas = append(linhas, fmt.Sprintf("  %s -> %s [%d/%d]: %s",
			c.Rota[t], c.Rota[t+1], c.Ocupados[t], c.Assentos, lista))
	}

	return linhas, nil
}

// MinhasReservas lista as reservas de um passageiro, prontas para exibir.
func (b *Banco) MinhasReservas(passageiro string) []string {
	b.mu.Lock()
	defer b.mu.Unlock()

	var linhas []string

	for _, r := range b.Reservas {
		if r.Passageiro != passageiro {
			continue
		}

		detalhe := ""
		switch {
		case r.Estado == "pendente":
			detalhe = fmt.Sprintf(" | expira em %s", time.Until(r.Expira).Round(time.Second))
		case r.Motivo != "":
			detalhe = " | " + r.Motivo
		}

		linhas = append(linhas, fmt.Sprintf("reserva %d | %s | %d trecho(s)%s",
			r.ID, r.Estado, len(r.Itens), detalhe))
	}

	return linhas
}

// ---------------------------------------------------------------------------
// Funcoes internas: NENHUMA trava. Sao chamadas de dentro das publicas,
// que ja estao segurando o mutex.
// ---------------------------------------------------------------------------

// buscarDiretas acha caronas que sozinhas levam de origem ate destino.
func (b *Banco) buscarDiretas(origem, destino, data string) []protocolo.Opcao {
	var opcoes []protocolo.Opcao

	for _, c := range b.Caronas {
		if c.Cancelada || c.Data != data {
			continue
		}

		de := c.indice(origem)
		ate := c.indice(destino)

		// As duas cidades precisam estar na rota, e a origem precisa vir
		// ANTES do destino: a carona so anda em um sentido.
		if de == -1 || ate == -1 || de >= ate {
			continue
		}

		livres := c.livres(de, ate)
		if livres < 1 {
			continue
		}

		opcoes = append(opcoes, protocolo.Opcao{
			Itens: []protocolo.Item{{CaronaID: c.ID, De: de, Ate: ate}},
			Preco: c.Preco * (ate - de),
			Resumo: fmt.Sprintf("direto: carona %d (%s) %s | %d livre(s)",
				c.ID, c.Motorista, strings.Join(c.Rota[de:ate+1], " -> "), livres),
		})
	}

	return opcoes
}

// buscarComConexao acha pares de caronas que, juntas, levam de origem ate
// destino trocando de veiculo numa cidade do meio.
//
// E aqui que a atomicidade do Reservar ganha sentido: uma opcao destas tem
// DOIS itens, e nao adianta conseguir o primeiro e perder o segundo.
func (b *Banco) buscarComConexao(origem, destino, data string) []protocolo.Opcao {
	var opcoes []protocolo.Opcao

	for _, primeira := range b.Caronas {
		if primeira.Cancelada || primeira.Data != data {
			continue
		}

		de := primeira.indice(origem)
		if de == -1 {
			continue
		}

		// Testa cada cidade depois da origem como ponto de baldeacao.
		for meio := de + 1; meio < len(primeira.Rota); meio++ {
			baldeacao := primeira.Rota[meio]

			// Se a primeira carona ja chega no destino, isso e viagem direta:
			// a outra funcao ja cuidou disso.
			if baldeacao == destino {
				continue
			}
			if primeira.livres(de, meio) < 1 {
				continue
			}

			for _, segunda := range b.Caronas {
				if segunda.ID == primeira.ID || segunda.Cancelada || segunda.Data != data {
					continue
				}

				de2 := segunda.indice(baldeacao)
				ate2 := segunda.indice(destino)
				if de2 == -1 || ate2 == -1 || de2 >= ate2 {
					continue
				}
				if segunda.livres(de2, ate2) < 1 {
					continue
				}

				opcoes = append(opcoes, protocolo.Opcao{
					Itens: []protocolo.Item{
						{CaronaID: primeira.ID, De: de, Ate: meio},
						{CaronaID: segunda.ID, De: de2, Ate: ate2},
					},
					Preco: primeira.Preco*(meio-de) + segunda.Preco*(ate2-de2),
					Resumo: fmt.Sprintf("conexao em %s: carona %d (%s) + carona %d (%s)",
						baldeacao, primeira.ID, primeira.Motorista, segunda.ID, segunda.Motorista),
				})
			}
		}
	}

	return opcoes
}

// cancelar desfaz uma reserva ativa: devolve os assentos e registra o motivo.
func (b *Banco) cancelar(r *Reserva, motivo string) {
	b.devolverAssentos(r)
	r.Estado = "cancelada"
	r.Motivo = motivo
}

// devolverAssentos libera um assento em cada trecho de cada item da reserva.
// Usada pela expiracao e pelos dois cancelamentos.
func (b *Banco) devolverAssentos(r *Reserva) {
	for _, item := range r.Itens {
		if c := b.acharCarona(item.CaronaID); c != nil {
			c.liberar(item.De, item.Ate)
		}
	}
}

// persistir grava o banco no arquivo. Se nao houver arquivo configurado
// (como nos testes), nao faz nada.
//
// Nao trava: quem chama ja esta com o mutex na mao. Gravar aqui dentro
// garante que o arquivo nunca pega o estado pela metade.
func (b *Banco) persistir() {
	if b.arquivo == "" {
		return
	}

	conteudo, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		fmt.Println("erro ao converter os dados:", err)
		return
	}

	if err := os.WriteFile(b.arquivo, conteudo, 0o644); err != nil {
		fmt.Println("erro ao gravar", b.arquivo, ":", err)
	}
}

// acharCarona devolve um PONTEIRO para a carona dentro do slice.
//
// Repare no "for i := range" em vez de "for _, c := range": o range por valor
// entrega uma COPIA de cada carona, e alterar a copia nao mudaria nada. Para
// modificar o elemento de verdade e preciso pegar o endereco dele.
func (b *Banco) acharCarona(id int) *Carona {
	for i := range b.Caronas {
		if b.Caronas[i].ID == id {
			return &b.Caronas[i]
		}
	}
	return nil
}

func (b *Banco) acharReserva(id int) *Reserva {
	for i := range b.Reservas {
		if b.Reservas[i].ID == id {
			return &b.Reservas[i]
		}
	}
	return nil
}

// ativa diz se a reserva ainda segura assentos.
func (r Reserva) ativa() bool {
	return r.Estado == "pendente" || r.Estado == "paga"
}

// usa diz se algum item da reserva e da carona indicada.
func (r Reserva) usa(caronaID int) bool {
	for _, item := range r.Itens {
		if item.CaronaID == caronaID {
			return true
		}
	}
	return false
}

// trechos diz quantos trechos a carona tem: uma cidade a menos que a rota.
func (c Carona) trechos() int {
	return len(c.Rota) - 1
}

// indice devolve a posicao da cidade na rota, ou -1 se ela nao estiver la.
func (c Carona) indice(cidade string) int {
	for i, nome := range c.Rota {
		if nome == cidade {
			return i
		}
	}
	return -1
}

// livres diz quantos assentos estao livres em TODO o caminho de "de" ate "ate".
//
// Viajar da cidade "de" ate a cidade "ate" usa os trechos de, de+1, ..., ate-1.
// O passageiro precisa do mesmo assento no caminho inteiro, entao o que vale e
// o trecho MAIS CHEIO: e ele que limita.
func (c Carona) livres(de, ate int) int {
	menor := c.Assentos

	for t := de; t < ate; t++ {
		livre := c.Assentos - c.Ocupados[t]
		if livre < menor {
			menor = livre
		}
	}

	return menor
}

// ocupar marca um assento em cada trecho do caminho.
func (c *Carona) ocupar(de, ate int) {
	for t := de; t < ate; t++ {
		c.Ocupados[t]++
	}
}

// liberar devolve um assento em cada trecho do caminho.
func (c *Carona) liberar(de, ate int) {
	for t := de; t < ate; t++ {
		c.Ocupados[t]--
	}
}
