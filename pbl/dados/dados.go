// Package dados guarda o estado do sistema e as regras que mexem nele.
//
// REGRA DE OURO DESTE PACOTE: toda funcao publica (maiuscula) abre com
// mu.Lock() e defer mu.Unlock(). Funcoes internas (minusculas) NUNCA travam,
// porque sao chamadas de dentro das publicas -- se elas travassem tambem, o
// programa congelaria (deadlock).
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

// TempoDeReserva e quanto tempo a reserva fica de pe sem ser paga.
// E variavel (nao constante) para os testes poderem encurtar isso.
var TempoDeReserva = 10 * time.Minute

// Usuario e quem usa o sistema. Tipo e "motorista" ou "passageiro".
type Usuario struct {
	Login string `json:"login"`
	Senha string `json:"senha"`
	Tipo  string `json:"tipo"`
}

// Carona e uma viagem oferecida por um motorista.
//
// Rota e a sequencia de cidades: ["Feira", "Salvador", "Ilheus"].
// Os TRECHOS sao os intervalos entre elas: Feira->Salvador e Salvador->Ilheus.
// Uma rota com N cidades tem N-1 trechos.
//
// Ocupados tem um contador por trecho. E o coracao da modelagem: a
// disponibilidade POR TRECHO sai naturalmente, sem estrutura extra.
type Carona struct {
	ID        int      `json:"id"`
	Motorista string   `json:"motorista"`
	Rota      []string `json:"rota"`
	Data      string   `json:"data"`
	Assentos  int      `json:"assentos"` // total de assentos, igual em todo trecho
	Preco     int      `json:"preco"`    // preco POR TRECHO
	Ocupados  []int    `json:"ocupados"` // len = len(Rota)-1
}

// Reserva e um pedido de assentos, que pode cobrir varias caronas.
type Reserva struct {
	ID         int              `json:"id"`
	Passageiro string           `json:"passageiro"`
	Itens      []protocolo.Item `json:"itens"`
	Estado     string           `json:"estado"` // "pendente" | "paga" | "expirada"
	Expira     time.Time        `json:"expira"`
}

// Banco guarda TODO o estado do sistema em memoria.
//
// O mutex protege tudo que esta declarado abaixo dele. Como varias goroutines
// (uma por cliente conectado) mexem neste mesmo Banco, sem o mutex duas
// compras simultaneas poderiam ler o mesmo assento livre e vende-lo duas vezes.
//
// "mu" e "arquivo" comecam com minuscula, entao o encoding/json os ignora --
// e o que queremos: nem a fechadura nem o caminho do arquivo fazem parte dos
// dados salvos.
type Banco struct {
	mu      sync.Mutex
	arquivo string

	Usuarios map[string]Usuario `json:"usuarios"`
	Caronas  []Carona           `json:"caronas"`
	Reservas []Reserva          `json:"reservas"`
	ProxID   int                `json:"prox_id"`
}

// NovoBanco cria o banco ja com usuarios de teste, sem persistencia.
//
// Devolve PONTEIRO (*Banco), nunca valor. Um sync.Mutex nao pode ser copiado:
// cada copia teria a sua propria fechadura, e proteger uma copia nao protegeria
// as outras. O proprio "go vet" reclama se voce tentar copiar.
func NovoBanco() *Banco {
	return &Banco{
		Usuarios: map[string]Usuario{
			"ana":   {Login: "ana", Senha: "123", Tipo: "motorista"},
			"davi":  {Login: "davi", Senha: "123", Tipo: "motorista"},
			"bruno": {Login: "bruno", Senha: "123", Tipo: "passageiro"},
			"carla": {Login: "carla", Senha: "123", Tipo: "passageiro"},
		},
		ProxID: 1,
	}
}

// Carregar le o banco de um arquivo JSON. Se o arquivo nao existir (primeira
// execucao), comeca do zero. A partir daqui toda alteracao e gravada sozinha.
//
// O enunciado proibe SGBD mas permite JSON: e exatamente isto.
func Carregar(caminho string) *Banco {
	b := NovoBanco()
	b.arquivo = caminho

	conteudo, err := os.ReadFile(caminho)
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

// ---------------------------------------------------------------------------
// Funcoes publicas: TODAS travam o mutex
// ---------------------------------------------------------------------------

// Autenticar confere login e senha. O segundo retorno diz se deu certo.
func (b *Banco) Autenticar(login, senha string) (Usuario, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	u, existe := b.Usuarios[login]
	if !existe {
		return Usuario{}, false
	}

	if u.Senha != senha {
		return Usuario{}, false
	}

	return u, true
}

// CadastrarUsuario cria uma conta nova.
//
// Repare que a checagem "ja existe" e a escrita no mapa acontecem DENTRO do
// mesmo Lock. Se a checagem ficasse fora, duas pessoas registrando o mesmo
// login ao mesmo tempo passariam as duas pela verificacao antes de qualquer
// uma escrever -- e a segunda sobrescreveria a primeira. E o mesmo raciocinio
// das duas fases do Reservar.
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

	b.mu.Lock()
	defer b.mu.Unlock()

	if _, existe := b.Usuarios[login]; existe {
		return fmt.Errorf("o usuario %q ja existe", login)
	}

	b.Usuarios[login] = Usuario{Login: login, Senha: senha, Tipo: tipo}
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
	}

	r.Estado = "paga"
	b.persistir()

	return nil
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

		for _, item := range r.Itens {
			if c := b.acharCarona(item.CaronaID); c != nil {
				c.liberar(item.De, item.Ate)
			}
		}

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
		linhas = append(linhas, fmt.Sprintf("carona %d | %s | %s | R$%d/trecho | ocupados por trecho: %v de %d",
			c.ID, c.Data, strings.Join(c.Rota, " -> "), c.Preco, c.Ocupados, c.Assentos))
	}

	return linhas
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
		if r.Estado == "pendente" {
			detalhe = fmt.Sprintf(" | expira em %s", time.Until(r.Expira).Round(time.Second))
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
		if c.Data != data {
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
		if primeira.Data != data {
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
				if segunda.ID == primeira.ID || segunda.Data != data {
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
