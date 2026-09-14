# Vaijunto — Plano de execução

## Contexto

O PBL vence **quinta-feira**, e a meta é ter o sistema **funcional amanhã (segunda)**. Isso permite separar duas coisas que não cabiam juntas: segunda é para fazer funcionar, terça em diante é para entender cada linha, provar a concorrência e empacotar.

Hoje existe apenas o servidor de eco em `pbl/` (servidor + cliente TCP, goroutine por conexão) — que já cobre **socket TCP e concorrência básica**, e que você já entende a fundo pelo guia.

O que falta é o domínio: login, caronas, busca por trecho, reserva atômica multi-trecho, expiração de 10 minutos, persistência em JSON, testes e Docker.

Duas decisões de projeto cortam a maior parte da complexidade:

1. **Um único mutex global** protegendo todo o estado. Sem locks por carona, sem locks aninhados, sem risco de deadlock. O efeito colateral é enorme: a atomicidade multi-trecho — a parte mais difícil do enunciado — **sai de graça**, porque tudo acontece dentro de um único `Lock`.
2. **Uma struct de pedido só**, com todos os campos possíveis. Nada de interfaces ou `json.RawMessage`. Feio no papel, trivial de entender e depurar.

---

## Onde está a nota

Se algo tiver que ser cortado, corte de baixo para cima.

| Prioridade | Item | Por quê |
|---|---|---|
| 1 | Reserva atômica + mutex | É o objetivo declarado da disciplina |
| 2 | Teste com `go test -race` | Prova objetiva de que você tratou concorrência |
| 3 | Socket TCP + goroutines | **Já pronto** no eco |
| 4 | Expiração de 10 min | Requisito explícito do enunciado |
| 5 | Login | Requisito explícito, mas trivial |
| 6 | Docker | Requisito explícito, e é só configuração |
| 7 | Persistência em JSON | Requisito, mas o menos arriscado de perder |
| 8 | Busca com conexão (2 trechos) | Enriquece muito a demonstração |
| 9 | Cliente CLI bonito | Não vale nota nenhuma |

---

## Estrutura final

```
pbl/
  go.mod                    module vaijunto
  protocolo/protocolo.go    o que trafega na rede
  dados/dados.go            estado + mutex + regras
  dados/dados_test.go       o teste de corrida
  servidor/main.go          rede e roteamento
  cliente/main.go           menus de terminal
  Dockerfile
  docker-compose.yml
```

Quatro pacotes. `servidor` e `cliente` continuam sendo `package main`; `protocolo` e `dados` são bibliotecas.

---

## Módulo `protocolo`

Resolve o problema de fronteira de mensagem: **uma linha = uma mensagem**, conteúdo em JSON.

```go
type Pedido struct {
    Acao    string   // "login", "cadastrar", "buscar", "reservar", "pagar"
    Usuario string
    Senha   string
    Rota     []string  // cadastrar
    Data     string
    Assentos int
    Preco    int
    Origem  string     // buscar
    Destino string
    Itens     []Item   // reservar
    ReservaID int      // pagar
}

type Item struct { CaronaID, De, Ate int }   // De/Ate são índices na rota
```

| Função | Assinatura | O que faz |
|---|---|---|
| `Ler` | `Ler(*bufio.Reader) (Pedido, error)` | Lê até `\n`, faz `json.Unmarshal` |
| `Enviar` | `Enviar(io.Writer, Resposta) error` | `json.Marshal` + `\n` |

`Resposta` carrega `OK bool`, `Erro string`, `Caronas []Carona`, `Itinerarios [][]Item`, `ReservaID int`, `Mensagem string`.

---

## Módulo `dados` — o coração

```go
type Banco struct {
    mu       sync.Mutex
    Usuarios map[string]Usuario
    Caronas  []Carona
    Reservas []Reserva
    proxID   int
}

type Carona struct {
    ID int; Motorista string
    Rota []string        // ["Feira", "Salvador", "Ilheus"]
    Data string
    Assentos int         // total por trecho
    Ocupados []int       // len = len(Rota)-1, um contador por trecho
}

type Reserva struct {
    ID int; Passageiro string
    Itens []protocolo.Item
    Estado string        // "pendente" | "paga" | "expirada"
    Expira time.Time
}
```

**A ideia central:** `Ocupados` é um contador por trecho. Reservar Feira→Ilhéus incrementa os trechos 0 e 1. Reservar Salvador→Ilhéus incrementa só o trecho 1. Disponibilidade por trecho sai naturalmente.

### Funções públicas (todas com `mu.Lock()` + `defer mu.Unlock()`)

| Função | Assinatura | Regra |
|---|---|---|
| `Autenticar` | `(login, senha string) (Usuario, bool)` | Compara com o mapa |
| `CadastrarCarona` | `(motorista string, rota []string, data string, assentos, preco int) int` | Cria `Ocupados` zerado com `len(rota)-1` |
| `Buscar` | `(origem, destino, data string) [][]Item` | Itinerários diretos e com 1 conexão |
| `Reservar` | `(passageiro string, itens []Item) (int, error)` | **Atômica** — ver abaixo |
| `Pagar` | `(reservaID int) error` | `pendente` → `paga`; recusa expirada |
| `ExpirarVencidas` | `()` | Devolve assentos de reservas vencidas |
| `Salvar` / `Carregar` | `(caminho string) error` | JSON em arquivo |

### `Reservar` — a função que vale a nota

Duas fases dentro de **um único** `Lock`:

```
1. FASE DE VERIFICAÇÃO — não altera nada
   para cada item, para cada trecho de De até Ate:
       se Ocupados[t] >= Assentos  ->  devolve erro, NADA mudou

2. FASE DE APLICAÇÃO — só roda se a fase 1 passou inteira
   para cada item, para cada trecho:  Ocupados[t]++

3. Cria Reserva{Estado: "pendente", Expira: now + 10min}
```

Verificar tudo antes de aplicar qualquer coisa é o que garante o "pegou um trecho não fica sem o outro". Como está tudo sob o mesmo mutex, nenhuma outra goroutine consegue se intrometer entre a fase 1 e a 2.

### Funções internas (sem lock — chamadas de dentro das públicas)

| Função | Para quê |
|---|---|
| `(c Carona) Indice(cidade string) int` | Posição na rota, ou -1 |
| `(c Carona) TemVaga(de, ate int) bool` | Todos os trechos da faixa têm assento |
| `(c *Carona) Ocupar(de, ate int)` | Incrementa a faixa. Ponteiro: modifica |
| `(c *Carona) Liberar(de, ate int)` | Decrementa. Usada na expiração |

**Regra de ouro:** função pública trava, função interna nunca trava. Se uma função com lock chamar outra com lock, o programa congela — é o deadlock mais comum de iniciante.

---

## Módulo `servidor`

Mantém a estrutura do eco. Só muda o que acontece dentro de `atender`.

| Função | O que faz |
|---|---|
| `main` | `Carregar` o JSON, subir goroutine de expiração, `Listen(":8080")`, laço de `Accept` |
| `atender(conn, banco)` | Laço: `protocolo.Ler` → `executar` → `protocolo.Enviar` |
| `executar(p, *sessao, banco) Resposta` | `switch p.Acao` — o roteador |
| `expirarPeriodicamente(banco)` | `time.Ticker` de 30s chamando `ExpirarVencidas` |

`sessao` é uma struct local da goroutine guardando quem logou. Como vive só dentro de `atender`, **não precisa de mutex** — cada conexão tem a sua.

Toda ação que não seja `login` checa `sessao.Usuario != ""` antes de executar.

---

## Módulo `cliente`

| Função | O que faz |
|---|---|
| `main` | Conecta, pede login, direciona para o menu do tipo |
| `pedir(p Pedido) Resposta` | Envia e espera a resposta. Usada por tudo |
| `menuMotorista` | Cadastrar carona, listar as minhas |
| `menuPassageiro` | Buscar, escolher itinerário, reservar, pagar |

Menu numerado lido do teclado. Sem firula.

---

## Cronograma

### Segunda — deixar funcional (~8h)

| Fase | Tempo | Entrega verificável |
|---|---|---|
| 1. `go.mod` para `vaijunto` + `protocolo` | 1h | Cliente manda JSON, servidor devolve JSON |
| 2. `dados` + `Autenticar` + login | 1h30 | Login funciona, senha errada é recusada |
| 3. `CadastrarCarona` + `Buscar` direto | 2h | Motorista cadastra, passageiro encontra |
| 4. **`Reservar` atômica** | 2h | Reserva de 2 trechos, tudo ou nada |
| 5. Expiração de 10 min | 1h | Assento volta sozinho |
| 6. Cliente com menus | 1h30 | Fluxo completo pelo terminal |

**Fim de segunda: o sistema roda de ponta a ponta na sua máquina.**

### Terça — entender e provar

| Fase | Tempo | Entrega |
|---|---|---|
| 7. Revisão linha por linha | 3h | Eu explico tudo que escrevemos, você pergunta |
| 8. **Teste com `-race`** | 1h30 | 50 goroutines no último assento; só 1 vence |

### Quarta — empacotar

| Fase | Tempo | Entrega |
|---|---|---|
| 9. Docker | 1h | `docker compose up` sobe tudo |
| 10. Persistência JSON | 45min | Reinicia sem perder dados |
| 11. Busca com conexão | 45min | Itinerário de 2 caronas |

### Quinta — entregar

README, roteiro de demonstração e revisão final. Folga proposital para o que atrasar.

### O teste da fase 8

```
carona com 1 assento
50 goroutines chamando Reservar ao mesmo tempo
sync.WaitGroup espera todas
verifica: exatamente 1 sucesso, 49 erros, Ocupados[0] == 1
```

Rode `go test -race ./...`. Depois **comente o `mu.Lock()`** e rode de novo: o detector acusa a corrida e a contagem sai errada. Esses dois resultados lado a lado são a sua demonstração na apresentação.

---

## Detalhes que economizam tempo

- **`":8080"` e não `"localhost:8080"`** no servidor — hoje está errado em `pbl/servidor/main.go:10` e quebraria no Docker.
- **Senha em texto puro** no JSON. É PBL, não produção. Não gaste tempo com hash.
- **Salvar em disco a cada alteração**, não periodicamente. Mais simples e evita mais um problema de concorrência.
- **`Data` como string** `"2026-09-14"`. Comparação exata, sem `time.Parse`.
- **Reaproveite o eco**: `bufio`, `defer`, o laço de `Accept` e a goroutine por cliente continuam idênticos.

---

## Como vamos trabalhar

**Segunda — ritmo de construção.** Eu escrevo cada fase inteira, com comentário curto explicando o essencial na hora. Você roda, confere que funciona, e seguimos. Sem parar para entender cada linha — o objetivo do dia é ver o sistema de pé.

**Terça — ritmo de estudo.** Voltamos ao código já pronto e funcionando e eu explico linha por linha, no formato do guia que você já tem. Aí sim você pergunta à vontade, e o que estiver confuso a gente reescreve junto.

Estudar código que **já funciona** é mais fácil do que estudar código que você ainda não viu rodar: você já sabe o que ele faz, e só precisa entender como.

**Ressalva:** se em algum momento de segunda você quiser parar e entender antes de seguir, é só falar. O cronograma tem folga na quinta justamente para isso.

---

## Verificação final

```
cd pbl
go vet ./...
go test -race ./...          # o teste de corrida passa
docker compose up --build    # servidor sobe
docker compose run cliente   # conecta e opera
```

Roteiro de demonstração: cadastrar carona com 1 assento → dois passageiros tentam reservar → um consegue → o que conseguiu não paga → 10 min depois o assento volta → o segundo consegue.
