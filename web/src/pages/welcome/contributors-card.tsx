import { welcomeContributors } from "./contributors";

export function WelcomeContributorsCard() {
    if (!welcomeContributors.length) return null;

    const founder = welcomeContributors.find((person) => person.isFounder);
    const rest = welcomeContributors.filter((person) => !person.isFounder);
    const groups = [
        founder ? { label: "发起", people: [founder], role: "项目发起者" } : null,
        rest.length ? { label: "参与", people: rest, role: "贡献者" } : null,
    ].filter(Boolean) as Array<{ label: string; people: typeof welcomeContributors; role: string }>;

    return (
        <section id="contributors" className="welcome-credits" aria-labelledby="welcome-credits-title">
            <h2 id="welcome-credits-title" className="welcome-credits-title">演职员<span>表</span></h2>
            <table className="welcome-credits-table">
                <thead>
                    <tr>
                        <th scope="col">组别</th>
                        <th scope="col">姓名</th>
                        <th scope="col">署名</th>
                        <th scope="col">身份</th>
                    </tr>
                </thead>
                <tbody>
                    {groups.flatMap((group) => group.people.map((person, index) => (
                        <tr key={person.name} className={index === 0 ? "is-group" : undefined}>
                            <td>{index === 0 ? group.label : ""}</td>
                            <td>{person.name}</td>
                            <td>{person.signature || "—"}</td>
                            <td>{group.role}</td>
                        </tr>
                    )))}
                </tbody>
            </table>
        </section>
    );
}
