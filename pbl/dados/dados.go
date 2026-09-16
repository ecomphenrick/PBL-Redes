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

// formatoData e o molde de data do Go: o layout e escrito com a data de
// referencia 2006-01-02, e o Go entende "ano-mes-dia com zeros".
const formatoData = "2006-01-02"

var ErrDataInvalida = errors.New("data invalida: use o formato AAAA-MM-DD (ex.: 2026-09-20)")

// DataValida diz se a data esta no formato AAAA-MM-DD e existe de verdade
// (2026-02-30 e recusada, porque time.Parse confere o calendario).
func DataValida(data string) bool {
	_, err := time.Parse(formatoData, data)
	return err == nil
}

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
	b.persistir()                                                       //banco inteiro em json

	return nil
}

func (b *Banco) CadastrarCarona(motorista string, rota []string, data string, assentos, preco int) (int, error) {
	if len(rota) < 2 {
		return 0, errors.New("a rota precisa de pelo menos 2 cidades")
	}
	if assentos < 1 {
		return 0, errors.New("a carona precisa de pelo menos 1 assento")
	}
	if !DataValida(data) {
		return 0, ErrDataInvalida
	}

	//apos conferir necessidades, trava o banco
	b.mu.Lock()
	defer b.mu.Unlock()

	c := Carona{
		ID:        b.ProxID,
		Motorista: motorista,
		Rota:      rota,
		Data:      data,
		Assentos:  assentos,
		Preco:     preco,
		Ocupados:  make([]int, len(rota)-1), // Um contador por trecho, zerado
	}

	//cria carona e coloca no banco
	b.ProxID++
	b.Caronas = append(b.Caronas, c)
	b.persistir() //persiste banco

	return c.ID, nil
}

func (b *Banco) Buscar(origem, destino, data string) []protocolo.Opcao {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.expirarVencidas()

	opcoes := b.buscarDiretas(origem, destino, data)
	return append(opcoes, b.buscarComConexao(origem, destino, data)...)
}

func (b *Banco) Reservar(passageiro string, itens []protocolo.Item) (int, error) {
	if len(itens) == 0 {
		return 0, errors.New("nenhum trecho escolhido")
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	b.expirarVencidas()

	for _, item := range itens {
		c := b.acharCarona(item.CaronaID)
		if c == nil {
			return 0, fmt.Errorf("carona %d nao existe", item.CaronaID) //se carona existe.
		}
		if c.Cancelada {
			return 0, fmt.Errorf("carona %d foi cancelada", item.CaronaID) //se cancelada
		}
		if item.De < 0 || item.Ate > c.trechos() || item.De >= item.Ate {
			return 0, fmt.Errorf("trecho invalido na carona %d", item.CaronaID)
		}
		if c.livres(item.De, item.Ate) < 1 {
			return 0, fmt.Errorf("sem assento livre na carona %d", item.CaronaID) //se tem assento livre.
		}
	}

	//se tudo ok: reserva
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

func (b *Banco) Pagar(passageiro string, reservaID int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.expirarVencidas()

	r := b.acharReserva(reservaID)

	//confere se a reserva é sua e se existe.
	if r == nil {
		return fmt.Errorf("reserva %d nao existe", reservaID)
	}
	if r.Passageiro != passageiro {
		return errors.New("esta reserva nao e sua")
	}

	//confere o estado, so pode ser paga se estiver pendente.
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

func (b *Banco) CancelarReserva(passageiro string, reservaID int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.expirarVencidas()

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

	//cancela reserva pelo passageiro
	b.cancelar(r, "cancelada pelo passageiro")
	b.persistir()

	return nil
}

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

	//true para carona cancelada
	c.Cancelada = true

	motivo := fmt.Sprintf("carona %d cancelada pelo motorista", caronaID)
	afetadas := 0

	//percorre todas as reservas e ve quais possuem o mesmo ID para cancelar
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

func (b *Banco) ExpirarVencidas() int {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.expirarVencidas()
}

// expirarVencidas e a versao interna, sem lock. As funcoes publicas que mexem
// com reservas chamam ela logo depois do Lock: assim uma reserva vencida
// expira NA HORA, sem esperar a goroutine de varredura passar.
func (b *Banco) expirarVencidas() int {
	agora := time.Now()
	expiradas := 0

	for i := range b.Reservas { //goroutine verificando isso a cada 30s
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

func (b *Banco) MinhasCaronas(motorista string) []string {
	b.mu.Lock()
	defer b.mu.Unlock()

	var linhas []string

	for _, c := range b.Caronas {
		if c.Motorista != motorista {
			continue //procura o motorista, motorista com mesmo nome pode dar problema.
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

func (b *Banco) PassageirosDaCarona(motorista string, caronaID int) ([]string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.expirarVencidas()

	c := b.acharCarona(caronaID)
	if c == nil {
		return nil, fmt.Errorf("carona %d nao existe", caronaID)
	}
	if c.Motorista != motorista {
		return nil, errors.New("esta carona nao e sua")
	}

	//verifica se existe e se é dele.

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

func (b *Banco) MinhasReservas(passageiro string) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.expirarVencidas()

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

		// Monta o caminho de cada item, para o passageiro saber que reserva e essa.
		var caminho []string
		for _, item := range r.Itens {
			if c := b.acharCarona(item.CaronaID); c != nil {
				caminho = append(caminho, fmt.Sprintf("%s -> %s (carona %d, %s)",
					c.Rota[item.De], c.Rota[item.Ate], c.ID, c.Data))
			}
		}

		linhas = append(linhas, fmt.Sprintf("reserva %d | %s | %s%s",
			r.ID, r.Estado, strings.Join(caminho, " + "), detalhe))
	}

	return linhas
}

func (b *Banco) buscarDiretas(origem, destino, data string) []protocolo.Opcao {
	var opcoes []protocolo.Opcao

	for _, c := range b.Caronas {
		if c.Cancelada || c.Data != data { //pula canceladas ou de outra data
			continue
		}

		de := c.indice(origem)
		ate := c.indice(destino)

		if de == -1 || ate == -1 || de >= ate {
			continue
		}

		livres := c.livres(de, ate)
		if livres < 1 {
			continue
		}

		opcoes = append(opcoes, protocolo.Opcao{
			Itens: []protocolo.Item{{CaronaID: c.ID, De: de, Ate: ate}},
			Preco: c.Preco * (ate - de), //preço por trecho * numero de trechos.
			Resumo: fmt.Sprintf("direto: carona %d (%s) %s | %d livre(s)",
				c.ID, c.Motorista, strings.Join(c.Rota[de:ate+1], " -> "), livres), //pega só o trecho relevante
		})
	}

	return opcoes
}

//ver melhor essas funções de buscar

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

		for meio := de + 1; meio < len(primeira.Rota); meio++ {
			baldeacao := primeira.Rota[meio]

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
func (b *Banco) devolverAssentos(r *Reserva) {
	for _, item := range r.Itens {
		if c := b.acharCarona(item.CaronaID); c != nil {
			c.liberar(item.De, item.Ate)
		}
	}
}

func (b *Banco) persistir() {
	if b.arquivo == "" {
		return
	}

	conteudo, err := json.MarshalIndent(b, "", "  ") //converte o banco em json
	if err != nil {
		fmt.Println("erro ao converter os dados:", err)
		return
	}

	if err := os.WriteFile(b.arquivo, conteudo, 0o644); err != nil { //útil no linux
		fmt.Println("erro ao gravar", b.arquivo, ":", err)
	}
}

func (b *Banco) acharCarona(id int) *Carona {
	for i := range b.Caronas {
		if b.Caronas[i].ID == id { //percorre as caronas pelo id, se achar retorna a carona
			return &b.Caronas[i]
		}
	}
	return nil
}

func (b *Banco) acharReserva(id int) *Reserva {
	for i := range b.Reservas {
		if b.Reservas[i].ID == id {
			return &b.Reservas[i] //mesma coisa da achar carona
		}
	}
	return nil
}

// ativa para segurar os assentos
func (r Reserva) ativa() bool {
	return r.Estado == "pendente" || r.Estado == "paga"
}

// a reserva usa a carona certa?
func (r Reserva) usa(caronaID int) bool {
	for _, item := range r.Itens {
		if item.CaronaID == caronaID {
			return true
		}
	}
	return false
}

// retorna qtd de trechos
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

// para verificar qual trecho tem menos vagas (se for 0 lock)
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
