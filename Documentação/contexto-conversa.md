# Vaijunto - Contexto da Conversa (transferencia de chat)

Documento gerado em 2026-08-28 para transferir o contexto desta conversa para um novo chat.
Contem: o estado do projeto, o que ja foi discutido, o que foi decidido e o que ainda esta em aberto.

---

## 1. O projeto

**Vaijunto: Sistema de Caronas Compartilhadas.**

- Atividade de faculdade (PBL - UEFS, Engenharia de Computacao, 5o semestre).
- Objetivo pedagogico declarado: aprender a tratar **concorrencia** e **conectividade**.
- Sistema de gerenciamento de caronas (estilo Uber / BlaBlaCar).
- Prazo: **1 mes**.
- Trabalho **individual**.
- Deve rodar em **Windows e Linux sem dependencia** (uso de conteineres).

## 2. Perfil do desenvolvedor (contexto para o tom das respostas)

- Aluno de Engenharia de Computacao, 5o semestre.
- Boa base em logica de programacao e algoritmos.
- **Zero conhecimento em servidores e concorrencia.**
- Pede linguagem objetiva e tecnica, mas explicada de forma que alguem sem conhecimento previo entenda.

## 3. Diretrizes de trabalho dadas pelo usuario

Extraidas de `Skills/plan.md`:

1. Discutir antes de planejar. Fazer perguntas tecnicas, conversar, escolher linguagem e arquitetura, e **so entao** escrever o plano.
2. **Responder as perguntas do usuario e so fazer outras perguntas depois que ele responder.** Uma pergunta de cada vez, sem despejar informacao.
3. O usuario quer entender **cada passo** que esta dando, sem pressa, dentro do prazo.
4. Usar versoes recentes de linguagens e bibliotecas, mas estabilizadas.
5. **Ser simples, quase como um iniciante. Sem over-engineering. Sempre simples.**
6. Ser conciso. Sem emojis.
7. Entregavel do planejamento: pasta `Documentação/Plano` com um PDF do plano e um README com o mesmo conteudo.
8. Apenas apos o planejamento o usuario vai preencher o `CLAUDE.md` para iniciar o desenvolvimento.

## 4. Estado dos arquivos

```
PBL Redes/
  pbl redes.pdf                          <- enunciado oficial (NAO LIDO - ver bloqueio abaixo)
  Documentação/
    Diagrama básico.drawio               <- LIDO, fonte dos requisitos abaixo
    Diagrama básico.drawio (2).pdf
    contexto-conversa.md                 <- este arquivo
  Skills/
    plan.md                              <- LIDO, diretrizes do usuario
    claude.md                            <- placeholder obsoleto (ver observacao)
```

**Observacao sobre o CLAUDE.md:** tanto `Skills/claude.md` quanto o `CLAUDE.md` carregado na raiz contem o texto de um projeto **Kanban em NextJS**, que nao tem nenhuma relacao com o Vaijunto. E um template deixado para tras. O usuario ja informou que vai preenche-lo depois do planejamento.

## 5. BLOQUEIO IMPORTANTE: o enunciado nao foi lido

O arquivo `pbl redes.pdf` (16 MB) e **escaneado** - contem apenas imagens, sem camada de texto. O ambiente atual nao tem `pdftoppm`, `pdfinfo`, PyMuPDF, pypdfium2 nem OCR, entao a extracao falhou (`pdftotext` retornou 3 bytes).

**Tudo o que se sabe sobre os requisitos veio do diagrama drawio.** Antes de fechar a arquitetura, e necessario que o usuario cole os trechos-chave do enunciado, principalmente:

- O enunciado **proibe frameworks HTTP** e exige socket cru?
- Existe exigencia sobre o formato do protocolo de aplicacao?
- Ha requisitos de entrega ou documentacao alem dos ja conhecidos?

## 6. Requisitos extraidos do diagrama

### Atores

- **SERVIDOR CENTRAL** - no meio, conversa com os dois lados.
- **MOTORISTA** (cliente) - autenticar; cadastrar rota (sequencia de cidades, em texto); data e horario; quantidade de assentos livres; valor por trecho.
- **PASSAGEIRO** (cliente) - autenticar; cidade de origem; cidade de destino; data desejada.

### Funcionalidades

- Disponibilidade **por trecho**.
- Passageiro pode pegar **mais de um transporte** para completar a viagem.
- **Atomico**: se pegou um trecho, nao pode ficar sem o outro. Ou reserva tudo, ou nada.
- Confirmou, o assento fica indisponivel.
- **Assento nao pode ficar preso se nao for pago: expira em 10 minutos.**
- Tratar **concorrencia de duas compras ao mesmo tempo**.
- Tela de login para motoristas e para passageiros.

### Requisitos tecnicos

- Implementar sobre o subsistema de rede **TCP/IP**.
- Suporte a **API socket basica**.
- Teste de software otimizado.
- Testado via **Docker**.
- **Nao e permitido usar SGBD. Pode usar JSON.**

### Onde esta a nota

Os pontos criticos de avaliacao sao: concorrencia (compra simultanea do mesmo assento), atomicidade multi-trecho, expiracao de reserva e uso correto de sockets. A interface visual nao vale nota.

---

## 7. O que ja foi discutido

### 7.1 Pergunta original: "Go ou JS?"

Resposta dada, com pros e contras de cada:

**Go - a favor**

- Concorrencia e o assunto central da linguagem: goroutines, canais, `sync.Mutex`, `sync.WaitGroup` nativos, com paralelismo real.
- `go test -race`: detector de condicao de corrida embutido, aponta a linha exata do conflito. Evidencia objetiva de tratamento de concorrencia para a apresentacao.
- Socket cru na biblioteca padrao (`net.Listen`, `net.Dial`), sem dependencia externa.
- Binario unico e estatico: roda em Windows e Linux sem instalar nada. Imagem Docker de 10 a 20 MB.
- Stdlib cobre tudo: `encoding/json` para o "banco", `time.AfterFunc` para o timer de 10 min, `testing`.
- Tipagem estatica ajuda numa maquina de estados com trechos, assentos e reservas.

**Go - contra**

- Linguagem nova (mitigado: Go e pequena, cerca de 25 palavras-chave).
- `if err != nil` verboso.
- Sem interface grafica natural: clientes seriam CLI.
- Menos tutorial em portugues.

**Node.js - a favor**

- Sintaxe ja conhecida pelo usuario, curva quase zero.
- O modulo `net` do Node tambem da socket TCP cru.
- Mesma linguagem no servidor e no cliente; caminho curto para web.
- Ecossistema npm.

**Node.js - contra (o ponto decisivo)**

- **Single-thread.** Condicoes de corrida classicas nao acontecem naturalmente. Isso enfraquece o objetivo pedagogico da disciplina e dificulta demonstrar tratamento de concorrencia na apresentacao. Restam apenas corridas logicas em `async/await` (ler estado, dar await, escrever depois que outro fluxo mexeu), um aprendizado mais fraco e menos visivel.
- Sem detector de corrida equivalente ao `-race`.
- Runtime e dependencia; imagem Docker de 50 a 150 MB contra 10 a 20 MB.
- Tipagem dinamica. TypeScript resolveria, mas adiciona build e config, contra a diretriz de simplicidade.

**Recomendacao dada: Go.**

### 7.2 Outras linguagens (usuario pediu para avaliar)

- **Python** - facil e legivel, mas o GIL impede paralelismo real entre threads (problema parecido com o do Node) e depende do interpretador instalado.
- **Java** - concorrencia excelente e madura, sockets na stdlib. Contras: muito verboso, JVM pesada no Docker, mais cerimonia do que o prazo pede.
- **C** - maximo controle e o que mais ensina sobre sockets de baixo nivel, mas gerenciamento manual de memoria e threads. Em um mes, sozinho, risco alto de gastar o tempo cacando segfault em vez de aprender concorrencia.

Conclusao: nenhuma supera Go nos criterios deste PBL.

### 7.3 Pergunta do usuario: "Algo como Go + JS, ambos?"

Fato tecnico que decide tudo: **navegador nao fala TCP cru** (so HTTP e WebSocket). Node.js fala TCP via modulo `net`. Entao "JS" significa duas coisas muito diferentes. Tres formatos possiveis:

**Formato A - Servidor Go + cliente Node no terminal**
Cliente Node com `net.connect()` falando o mesmo protocolo do servidor. Funciona e atende o requisito de socket.
Problema: ganha JS mas continua com interface de terminal. Paga o custo de duas linguagens sem ganhar a interface bonita, que era o motivo de querer JS.

**Formato B - Servidor Go + cliente Electron**
Electron e Node com uma janela de navegador em volta; o processo principal consegue abrir socket TCP e mandar os dados para a tela em HTML/CSS. Unica forma de ter interface grafica bonita e socket cru ao mesmo tempo.
Problema: 150 a 200 MB por cliente, empacotamento para Windows e Linux trabalhoso, adiciona npm e build. Contra a diretriz de simplicidade.

**Formato C - Servidor Go falando HTTP/WebSocket + pagina web comum**
Mais confortavel de desenvolver.
Problema serio: provavelmente **descumpre o requisito** de "suporte a API socket basica". Precisa confirmar no PDF.

**Recomendacao dada:** Go puro no MVP (servidor + clientes CLI), com interface web como **fase extra opcional** no fim do mes, se sobrar tempo. Assim a interface vira bonus, nao risco de prazo.

### 7.4 Curva de aprendizado do Go (topico pedido pelo usuario)

Premissa: o usuario nao precisa aprender Go inteira, apenas um subconjunto pequeno.

| Topico | Para que serve no Vaijunto | Tempo |
|---|---|---|
| Sintaxe base, `struct`, `slice`, `map` | Modelar Carona, Trecho, Assento, Reserva | 2 dias |
| `error` e `if err != nil` | Todo retorno de rede e arquivo | junto |
| `goroutine` | Uma por cliente conectado | 1 dia |
| `sync.Mutex` | Proteger o mapa de assentos da compra dupla | 1 dia |
| `channel` | Comunicacao entre goroutines, timeout | junto |
| `net` (TCP) | `net.Listen` e `net.Dial` | 1 dia |
| `encoding/json` | O "banco de dados" em arquivo | 2 horas |
| `time.AfterFunc` | Expirar a reserva em 10 minutos | 1 hora |
| `testing` e `go test -race` | Provar que a concorrencia esta correta | 1 dia |

**Total realista: 5 a 7 dias**, estudando e aplicando ao mesmo tempo, nao parado lendo antes de comecar.

**Pode ignorar completamente:** generics, reflection, `context` avancado, interfaces sofisticadas, qualquer framework.

**As tres coisas que vao confundir:**

1. **Ponteiro vs valor** (`*Carona` contra `Carona`). Go copia structs por padrao; a alteracao "nao acontece". Regra pratica: se precisa modificar, use ponteiro.
2. **`if err != nil` em toda linha.** Verbosidade real, mas e digitacao, nao raciocinio.
3. **Goroutine que morre sem avisar.** Um panico dentro de goroutine derruba o programa inteiro. Aprende-se a tratar uma vez.

**Divisao do mes sugerida:** 1 semana aprendendo Go construindo as fundacoes, 2 semanas para o sistema completo, 1 semana para testes, Docker e documentacao.

---

## 8. Decisoes e pendencias

### Decidido

Nada foi formalmente fechado. A conversa estava em andamento.

### Recomendado (aguardando confirmacao do usuario)

- **Linguagem: Go.**
- **Arquitetura: Go puro no MVP**, servidor mais clientes CLI, com interface web como fase extra opcional no fim.

### Em aberto

1. O usuario ainda **nao escolheu a linguagem**. Quando perguntado, respondeu "ainda quero discutir".
2. A ultima pergunta feita a ele (como seguir com o hibrido Go + JS: Go puro / Go + Electron / confirmar enunciado antes) **foi interrompida e nao respondida**.
3. **O enunciado em PDF nao foi lido.** Necessario antes de fechar a arquitetura.
4. Ainda nao discutido: formato do protocolo de aplicacao, modelo de persistencia em JSON, estrategia de testes, estrutura dos conteineres Docker.

---

## 9. Como continuar no proximo chat

1. Confirmar a linguagem com o usuario (recomendacao: Go).
2. Pedir a ele o conteudo do `pbl redes.pdf`, ou ao menos os trechos sobre socket, restricoes de biblioteca e entregaveis. **O PDF e escaneado e nao pode ser lido pelas ferramentas do ambiente.**
3. Confirmar a arquitetura (recomendacao: Go puro, web opcional no fim).
4. So depois disso, discutir protocolo, persistencia, testes e Docker.
5. Por ultimo, escrever o plano em `Documentação/Plano/` (README.md mais PDF), com criterios de sucesso por fase.

**Lembrar sempre:** uma pergunta por vez, esperar a resposta, ser simples, sem over-engineering, sem emojis.
