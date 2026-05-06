namespace Comprehensive.Interfaces
{
    public interface IService
    {
        string Execute(string input);
        bool IsReady { get; }
    }

    public interface IAsyncService
    {
        System.Threading.Tasks.Task<string> ExecuteAsync(string input);
    }

    public interface IRepository<T> where T : class
    {
        T FindById(string id);
        System.Collections.Generic.IEnumerable<T> FindAll();
        void Save(T entity);
        void Delete(string id);
    }
}
