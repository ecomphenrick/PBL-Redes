# Vaijunto

Sistema de caronas compartilhadas sobre sockets TCP, em Go, sem dependências
externas. Um servidor central guarda todo o estado; motoristas e passageiros
usam o mesmo programa cliente.

## Requisitos

- Go 1.27 (para rodar direto), ou
- Docker e Docker Compose (para rodar em contêiner)

## Estrutura

```
pbl/
  protocolo/   mensagens trocadas na rede (Pedido, Resposta, Item, Opcao)
  dados/       estado do sistema, mutex e regras de negócio
  servidor/    aceita conexões e roteia as ações
  cliente/     menus de terminal
  carga/       teste de carga com N clientes simultâneos
  Dockerfile
  docker-compose.yml
```

Os pacotes `protocolo` e `dados` são bibliotecas; `servidor`, `cliente` e
`carga` são programas (`package main`). O módulo se chama `vaijunto`.

## Como executar

### Com Go

```
cd pbl
go run ./servidor      # terminal 1
go run ./cliente       # terminal 2 (pode abrir vários)
```

### Com Docker

```
cd pbl
docker compose build
docker compose up -d servidor      # liga o servidor
docker compose run --rm cliente    # abre um cliente (repita em vários terminais)
docker compose down                # desliga (os dados ficam no volume)
```

Em outra máquina, o cliente precisa do IP da máquina do servidor:

```
docker compose run --rm --no-deps -e VAIJUNTO_SERVIDOR=IP_DO_SERVIDOR:8080 cliente
```

## Como usar

O banco começa vazio: crie as contas pela opção **2) criar conta**, escolhendo
motorista ou passageiro.

| Usuário | Ações |
|---|---|
| Motorista | cadastrar carona; listar as suas caronas e detalhar os passageiros de cada trecho; cancelar carona |
| Passageiro | buscar viagens (diretas e com uma baldeação); reservar; pagar; listar reservas; cancelar reserva |

A rota é digitada como cidades separadas por vírgula (`Feira, Salvador, Ilheus`)
e a data no formato `AAAA-MM-DD`. A reserva fica pendente por 30 segundos: se
não for paga, os assentos voltam para a fila.

## Testes

```
go test ./...                              # testes automatizados
go run ./carga -clientes 200               # teste de carga (com o servidor rodando)
go run ./carga -servidor IP:8080 -clientes 500
```

Com Docker, o teste de carga roda pela imagem do cliente:

```
docker compose run --rm cliente /bin/carga -servidor servidor:8080 -clientes 200
```

## Variáveis de ambiente

| Variável | Padrão | Para quê |
|---|---|---|
| `VAIJUNTO_RESERVA` | `30s` | prazo para pagar antes de o assento voltar |
| `VAIJUNTO_VARREDURA` | `5s` | frequência da checagem de reservas vencidas |
| `VAIJUNTO_DADOS` | `vaijunto.json` | arquivo onde o estado é gravado |
| `VAIJUNTO_SERVIDOR` | `localhost:8080` | onde o cliente e a carga procuram o servidor |



## Como funciona

- **Protocolo:** uma linha de JSON por mensagem, terminada em `\n`. O pedido
  sempre traz o campo `acao`; a resposta sempre traz `ok`.
- **Concorrência:** uma goroutine por conexão, todas usando o mesmo banco,
  protegido por um `sync.Mutex` global. Funções públicas travam; internas não.
- **Reserva atômica:** uma reserva pode cobrir vários trechos e até duas
  caronas. Primeiro todos os trechos são verificados, depois todos são
  ocupados, dentro do mesmo lock: ou tudo é reservado, ou nada muda.
- **Assentos por trecho:** cada carona guarda um contador de ocupação por
  trecho, então quem desce no meio libera o lugar para o restante da rota.
- **Persistência:** o estado é gravado em JSON a cada alteração e carregado
  quando o servidor sobe. No Docker, o arquivo fica num volume.
