using System;
using System.Collections.Generic;
using System.Linq;
using Comprehensive.Interfaces;

namespace Comprehensive.Generics
{
    public class InMemoryRepository<T> : IRepository<T> where T : class
    {
        private readonly Dictionary<string, T> _store = new();

        public T FindById(string id)
        {
            return _store.GetValueOrDefault(id);
        }

        public IEnumerable<T> FindAll()
        {
            return _store.Values;
        }

        public void Save(T entity)
        {
            var id = entity.GetHashCode().ToString();
            _store[id] = entity;
        }

        public void Delete(string id)
        {
            _store.Remove(id);
        }
    }
}
