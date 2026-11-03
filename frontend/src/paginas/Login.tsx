import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { api } from "../api";

export function Login({ onEntrou }: { onEntrou: () => void }) {
  const [email, setEmail] = useState("");
  const [senha, setSenha] = useState("");

  const entrar = useMutation({
    mutationFn: () => api.entrar(email, senha),
    onSuccess: onEntrou,
  });

  return (
    <main>
      <form
        className="cartao login"
        onSubmit={(e) => {
          e.preventDefault();
          entrar.mutate();
        }}
      >
        <h2>Cupons ITD</h2>
        <p className="ajuda">Entre para importar e revisar cupons.</p>

        <div style={{ marginBottom: "0.75rem" }}>
          <label htmlFor="email">E-mail</label>
          <input
            id="email"
            type="email"
            autoComplete="username"
            required
            value={email}
            onChange={(e) => setEmail(e.target.value)}
          />
        </div>

        <div style={{ marginBottom: "1rem" }}>
          <label htmlFor="senha">Senha</label>
          <input
            id="senha"
            type="password"
            autoComplete="current-password"
            required
            value={senha}
            onChange={(e) => setSenha(e.target.value)}
          />
        </div>

        {entrar.isError && <div className="erro">{(entrar.error as Error).message}</div>}

        <button className="acao" type="submit" disabled={entrar.isPending} style={{ width: "100%" }}>
          {entrar.isPending ? "Entrando…" : "Entrar"}
        </button>
      </form>
    </main>
  );
}
